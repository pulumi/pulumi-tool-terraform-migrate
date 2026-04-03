// Copyright 2016-2025, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hcl

import (
	"regexp"
	"sort"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func parseExpr(t *testing.T, src string) hcl.Expression {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{})
	require.False(t, diags.HasErrors(), diags.Error())
	return expr
}

// ctyStrSlice extracts []string from a cty list or set value.
func ctyStrSlice(val cty.Value) []string {
	var result []string
	it := val.ElementIterator()
	for it.Next() {
		_, v := it.Element()
		result = append(result, v.AsString())
	}
	return result
}

// ctyNumFloat extracts float64 from a cty number value.
func ctyNumFloat(val cty.Value) float64 {
	f, _ := val.AsBigFloat().Float64()
	return f
}

// evalStr is a test helper that evaluates an expression and returns the string result.
func evalStr(t *testing.T, ctx *EvalContext, expr string) string {
	t.Helper()
	val, err := ctx.EvaluateExpression(parseExpr(t, expr))
	require.NoError(t, err)
	return val.AsString()
}

// --- EvalContext tests (variables, refs, interpolation) ---

func TestEvaluateLiteral(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `"10.0.0.0/16"`))
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", val.AsString())
}

func TestEvaluateVariableRef(t *testing.T) {
	t.Parallel()
	vars := map[string]cty.Value{"cidr": cty.StringVal("10.0.0.0/16")}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, "var.cidr"))
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", val.AsString())
}

func TestEvaluateResourceRef(t *testing.T) {
	t.Parallel()
	resources := map[string]map[string]cty.Value{
		"random_pet": {"this": cty.ObjectVal(map[string]cty.Value{
			"id":        cty.StringVal("test-0-creative-doberman"),
			"prefix":    cty.StringVal("test-0"),
			"separator": cty.StringVal("-"),
		})},
	}
	ctx := NewEvalContext(nil, resources, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, "random_pet.this.id"))
	require.NoError(t, err)
	require.Equal(t, "test-0-creative-doberman", val.AsString())
}

func TestEvaluateModuleOutputRef(t *testing.T) {
	t.Parallel()
	moduleOutputs := map[string]map[string]cty.Value{
		"pet": {"name": cty.StringVal("test-0-creative-doberman")},
	}
	ctx := NewEvalContext(nil, nil, moduleOutputs)
	val, err := ctx.EvaluateExpression(parseExpr(t, "module.pet.name"))
	require.NoError(t, err)
	require.Equal(t, "test-0-creative-doberman", val.AsString())
}

func TestEvaluateConditional(t *testing.T) {
	t.Parallel()
	vars := map[string]cty.Value{"enable": cty.True}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `var.enable ? "yes" : "no"`))
	require.NoError(t, err)
	require.Equal(t, "yes", val.AsString())
}

func TestEvaluateForExpression(t *testing.T) {
	t.Parallel()
	vars := map[string]cty.Value{
		"names": cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
	}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `[for s in var.names : upper(s)]`))
	require.NoError(t, err)
	require.Equal(t, []string{"A", "B"}, ctyStrSlice(val))
}

func TestEvaluateCountIndex(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)
	ctx.AddVariables(map[string]cty.Value{
		"count": cty.ObjectVal(map[string]cty.Value{
			"index": cty.NumberIntVal(3),
		}),
	})
	val, err := ctx.EvaluateExpression(parseExpr(t, `"prefix-${count.index}"`))
	require.NoError(t, err)
	require.Equal(t, "prefix-3", val.AsString())
}

func TestEvaluateEachKey(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)
	ctx.AddVariables(map[string]cty.Value{
		"each": cty.ObjectVal(map[string]cty.Value{
			"key":   cty.StringVal("alpha"),
			"value": cty.StringVal("alpha"),
		}),
	})
	val, err := ctx.EvaluateExpression(parseExpr(t, `each.key`))
	require.NoError(t, err)
	require.Equal(t, "alpha", val.AsString())
}

func TestEvaluateUnsupportedFunction(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)
	_, err := ctx.EvaluateExpression(parseExpr(t, `totally_fake_function("x")`))
	require.Error(t, err)
}

func TestEvaluateStringInterpolation(t *testing.T) {
	t.Parallel()
	vars := map[string]cty.Value{"prefix": cty.StringVal("test")}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `"${var.prefix}-0"`))
	require.NoError(t, err)
	require.Equal(t, "test-0", val.AsString())
}

// --- Function evaluation tests: string results ---

func TestFunctionEval_StringResults(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)

	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		// String functions
		{"chomp", `chomp("hello\n")`, "hello"},
		{"format", `format("Hello, %s!", "world")`, "Hello, world!"},
		{"formatdate", `formatdate("YYYY-MM-DD", "2023-01-15T00:00:00Z")`, "2023-01-15"},
		{"indent", `indent(2, "a\nb")`, "a\n  b"},
		{"join", `join("-", ["a", "b"])`, "a-b"},
		{"lower", `lower("HELLO")`, "hello"},
		{"upper", `upper("hello")`, "HELLO"},
		{"regex", `regex("^[a-z]+", "hello123")`, "hello"},
		{"replace", `replace("hello-world", "-", "_")`, "hello_world"},
		{"strrev", `strrev("hello")`, "olleh"},
		{"substr", `substr("hello", 0, 3)`, "hel"},
		{"title", `title("hello world")`, "Hello World"},
		{"trim", `trim("  hello  ", " ")`, "hello"},
		{"trimprefix", `trimprefix("helloworld", "hello")`, "world"},
		{"trimsuffix", `trimsuffix("helloworld", "world")`, "hello"},
		{"trimspace", `trimspace("  hello  ")`, "hello"},
		// Collection → string
		{"coalesce", `coalesce("a", "b")`, "a"},
		{"element", `element(["a", "b"], 0)`, "a"},
		{"lookup", `lookup({a = "1"}, "a", "default")`, "1"},
		{"one", `one(toset(["a"]))`, "a"},
		// Encoding
		{"base64decode", `base64decode("aGVsbG8=")`, "hello"},
		{"base64encode", `base64encode("hello")`, "aGVsbG8="},
		{"base64gunzip", `base64gunzip(base64gzip("hello"))`, "hello"},
		{"jsonencode", `jsonencode({key = "value"})`, `{"key":"value"}`},
		{"textdecodebase64", `textdecodebase64(textencodebase64("hello", "UTF-8"), "UTF-8")`, "hello"},
		{"textencodebase64", `textencodebase64("hello", "UTF-8")`, "aGVsbG8="},
		{"urlencode", `urlencode("hello world")`, "hello+world"},
		{"urldecode", `urldecode("hello+world")`, "hello world"},
		{"yamlencode", `yamlencode({key = "value"})`, "\"key\": \"value\"\n"},
		// Filesystem (pure path manipulation, no I/O)
		{"dirname", `dirname("/path/to/file.txt")`, "/path/to"},
		{"basename", `basename("/path/to/file.txt")`, "file.txt"},
		{"pathexpand", `pathexpand(".")`, "."},
		// Network
		{"cidrhost", `cidrhost("10.0.0.0/8", 1)`, "10.0.0.1"},
		{"cidrnetmask", `cidrnetmask("10.0.0.0/8")`, "255.0.0.0"},
		{"cidrsubnet", `cidrsubnet("10.0.0.0/16", 8, 1)`, "10.0.1.0/24"},
		// Date/Time
		{"timeadd", `timeadd("2023-01-01T00:00:00Z", "1h")`, "2023-01-01T01:00:00Z"},
		// Crypto (deterministic for fixed input)
		{"base64sha256", `base64sha256("hello")`, "LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="},
		{"base64sha512", `base64sha512("hello")`, "m3HSJL1i83hdltRq0+o9czGb+8KJDKra4t/3JRlnPKcjI8PZm6XBHXx6zG4UuMXaDEZjR1wuXDre9G9zvN7AQw=="},
		{"md5", `md5("hello")`, "5d41402abc4b2a76b9719d911017c592"},
		{"sha1", `sha1("hello")`, "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"},
		{"sha256", `sha256("hello")`, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"sha512", `sha512("hello")`, "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043"},
		{"uuidv5", `uuidv5("dns", "example.com")`, "cfbff0d1-9375-5685-968c-48ce8b15ae17"},
		// Type conversion
		{"tostring", `tostring(42)`, "42"},
		// Filesystem (I/O with stable testdata)
		{"templatefile", `templatefile("testdata/template.txt", {name = "world"})`, "Hello, world!"},
		{"templatestring", `templatestring("Hello, $${name}!", {name = "world"})`, "Hello, world!"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			val, err := ctx.EvaluateExpression(parseExpr(t, tc.expr))
			require.NoError(t, err)
			require.Equal(t, tc.expected, val.AsString())
		})
	}
}

// --- Function evaluation tests: numeric results ---

func TestFunctionEval_NumericResults(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)

	tests := []struct {
		name     string
		expr     string
		expected float64
	}{
		// Math
		{"abs", `abs(-1)`, 1},
		{"ceil", `ceil(1.5)`, 2},
		{"floor", `floor(1.5)`, 1},
		{"log", `log(100, 10)`, 2},
		{"max", `max(1, 2, 3)`, 3},
		{"min", `min(1, 2, 3)`, 1},
		{"pow", `pow(2, 3)`, 8},
		{"signum", `signum(-5)`, -1},
		{"parseint", `parseint("10", 10)`, 10},
		// Collection → number
		{"index", `index(["a", "b"], "b")`, 1},
		{"length", `length(["a", "b"])`, 2},
		{"sum", `sum([1, 2, 3])`, 6},
		// Type conversion
		{"tonumber", `tonumber("42")`, 42},
		// Date/Time
		{"timecmp", `timecmp("2023-01-01T00:00:00Z", "2023-01-02T00:00:00Z")`, -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			val, err := ctx.EvaluateExpression(parseExpr(t, tc.expr))
			require.NoError(t, err)
			require.InDelta(t, tc.expected, ctyNumFloat(val), 0.0001)
		})
	}
}

// --- Function evaluation tests: boolean results ---

func TestFunctionEval_BoolResults(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)

	tests := []struct {
		name     string
		expr     string
		expected bool
	}{
		// String
		{"startswith", `startswith("hello", "hel")`, true},
		{"endswith", `endswith("hello", "llo")`, true},
		{"strcontains", `strcontains("hello", "ell")`, true},
		// Collection
		{"alltrue", `alltrue([true, true])`, true},
		{"anytrue", `anytrue([true, false])`, true},
		{"contains", `contains(["a", "b"], "a")`, true},
		// Network
		{"cidrcontains", `cidrcontains("10.0.0.0/8", "10.0.0.1")`, true},
		// Type conversion
		{"can", `can(true)`, true},
		{"try", `try(true, false)`, true},
		{"tobool", `tobool("true")`, true},
		{"issensitive", `issensitive("hello")`, false},
		// Filesystem
		{"fileexists", `fileexists("testdata/hello.txt")`, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			val, err := ctx.EvaluateExpression(parseExpr(t, tc.expr))
			require.NoError(t, err)
			if tc.expected {
				require.True(t, val.True())
			} else {
				require.True(t, val.False())
			}
		})
	}
}

// --- Function evaluation tests: collection results ---

func TestFunctionEval_CollectionResults(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)

	tests := []struct {
		name  string
		expr  string
		check func(t *testing.T, val cty.Value)
	}{
		// String → list
		{"formatlist", `formatlist("hello %s", ["a", "b"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"hello a", "hello b"}, ctyStrSlice(val))
		}},
		{"regexall", `regexall("[a-z]+", "hello123world")`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"hello", "world"}, ctyStrSlice(val))
		}},
		{"split", `split("-", "a-b-c")`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a", "b", "c"}, ctyStrSlice(val))
		}},
		// Collection functions
		{"chunklist", `chunklist(["a", "b", "c"], 2)`, func(t *testing.T, val cty.Value) {
			require.Equal(t, 2, val.LengthInt())
			chunks := val.AsValueSlice()
			require.Equal(t, []string{"a", "b"}, ctyStrSlice(chunks[0]))
			require.Equal(t, []string{"c"}, ctyStrSlice(chunks[1]))
		}},
		{"coalescelist", `coalescelist(["a"], ["b"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a"}, ctyStrSlice(val))
		}},
		{"compact", `compact(["a", "", "b"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a", "b"}, ctyStrSlice(val))
		}},
		{"concat", `concat(["a"], ["b"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a", "b"}, ctyStrSlice(val))
		}},
		{"distinct", `distinct(["a", "a", "b"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a", "b"}, ctyStrSlice(val))
		}},
		{"flatten", `flatten([["a"], ["b", "c"]])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a", "b", "c"}, ctyStrSlice(val))
		}},
		{"keys", `keys({a = "1", b = "2"})`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a", "b"}, ctyStrSlice(val))
		}},
		{"matchkeys", `matchkeys(["a", "b"], ["1", "2"], ["1"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a"}, ctyStrSlice(val))
		}},
		{"range", `range(3)`, func(t *testing.T, val cty.Value) {
			require.Equal(t, 3, val.LengthInt())
			nums := val.AsValueSlice()
			require.InDelta(t, 0, ctyNumFloat(nums[0]), 0.0001)
			require.InDelta(t, 1, ctyNumFloat(nums[1]), 0.0001)
			require.InDelta(t, 2, ctyNumFloat(nums[2]), 0.0001)
		}},
		{"reverse", `reverse(["a", "b"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"b", "a"}, ctyStrSlice(val))
		}},
		{"slice", `slice(["a", "b", "c"], 0, 2)`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a", "b"}, ctyStrSlice(val))
		}},
		{"sort", `sort(["b", "a"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a", "b"}, ctyStrSlice(val))
		}},
		{"values", `values({a = "1", b = "2"})`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"1", "2"}, ctyStrSlice(val))
		}},
		// Set functions
		{"setintersection", `setintersection(toset(["a", "b"]), toset(["a"]))`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a"}, ctyStrSlice(val))
		}},
		{"setproduct", `setproduct(["a"], ["b"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, 1, val.LengthInt())
			inner := val.AsValueSlice()[0]
			require.Equal(t, []string{"a", "b"}, ctyStrSlice(inner))
		}},
		{"setsubtract", `setsubtract(toset(["a", "b"]), toset(["b"]))`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a"}, ctyStrSlice(val))
		}},
		{"setunion", `setunion(toset(["a"]), toset(["b"]))`, func(t *testing.T, val cty.Value) {
			got := ctyStrSlice(val)
			sort.Strings(got)
			require.Equal(t, []string{"a", "b"}, got)
		}},
		{"tolist", `tolist(["a"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a"}, ctyStrSlice(val))
		}},
		{"toset", `toset(["a"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"a"}, ctyStrSlice(val))
		}},
		// Network → list
		{"cidrsubnets", `cidrsubnets("10.0.0.0/16", 8, 8)`, func(t *testing.T, val cty.Value) {
			require.Equal(t, []string{"10.0.0.0/24", "10.0.1.0/24"}, ctyStrSlice(val))
		}},
		// Map/object functions
		{"merge", `merge({a = "1"}, {b = "2"})`, func(t *testing.T, val cty.Value) {
			require.Equal(t, "1", val.GetAttr("a").AsString())
			require.Equal(t, "2", val.GetAttr("b").AsString())
		}},
		{"zipmap", `zipmap(["a", "b"], ["1", "2"])`, func(t *testing.T, val cty.Value) {
			require.Equal(t, "1", val.GetAttr("a").AsString())
			require.Equal(t, "2", val.GetAttr("b").AsString())
		}},
		{"tomap", `tomap({a = "1"})`, func(t *testing.T, val cty.Value) {
			m := val.AsValueMap()
			require.Equal(t, "1", m["a"].AsString())
		}},
		{"transpose", `transpose({a = ["1", "2"], b = ["1"]})`, func(t *testing.T, val cty.Value) {
			m := val.AsValueMap()
			got1 := ctyStrSlice(m["1"])
			sort.Strings(got1)
			require.Equal(t, []string{"a", "b"}, got1)
			require.Equal(t, []string{"a"}, ctyStrSlice(m["2"]))
		}},
		// Encoding → complex types
		{"csvdecode", `csvdecode("a,b\n1,2")`, func(t *testing.T, val cty.Value) {
			require.Equal(t, 1, val.LengthInt())
			row := val.AsValueSlice()[0]
			require.Equal(t, "1", row.GetAttr("a").AsString())
			require.Equal(t, "2", row.GetAttr("b").AsString())
		}},
		{"jsondecode", `jsondecode("{\"key\": \"value\"}")`, func(t *testing.T, val cty.Value) {
			require.Equal(t, "value", val.GetAttr("key").AsString())
		}},
		{"yamldecode", `yamldecode("key: value")`, func(t *testing.T, val cty.Value) {
			require.Equal(t, "value", val.GetAttr("key").AsString())
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			val, err := ctx.EvaluateExpression(parseExpr(t, tc.expr))
			require.NoError(t, err)
			tc.check(t, val)
		})
	}
}

// --- Function evaluation tests: filesystem I/O ---
// Uses testdata/hello.txt (content: "hello", no trailing newline) for stable hashes.

func TestFunctionEval_Filesystem(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)

	t.Run("abspath", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `abspath(".")`))
		require.NoError(t, err)
		// Returns an absolute path — just verify it's non-empty and absolute.
		assert.NotEmpty(t, val.AsString())
		assert.True(t, val.AsString()[0] == '/', "expected absolute path, got %q", val.AsString())
	})

	t.Run("file", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `file("testdata/hello.txt")`))
		require.NoError(t, err)
		require.Equal(t, "hello", val.AsString())
	})

	t.Run("filebase64", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `filebase64("testdata/hello.txt")`))
		require.NoError(t, err)
		require.Equal(t, "aGVsbG8=", val.AsString())
	})

	t.Run("fileset", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `fileset(".", "*.go")`))
		require.NoError(t, err)
		got := ctyStrSlice(val)
		assert.Contains(t, got, "evaluator.go")
		assert.Contains(t, got, "evaluator_test.go")
	})

	// File hash functions — all use testdata/hello.txt which contains "hello" (same hashes as md5("hello") etc.)
	fileHashTests := []struct {
		name     string
		expr     string
		expected string
	}{
		{"filemd5", `filemd5("testdata/hello.txt")`, "5d41402abc4b2a76b9719d911017c592"},
		{"filesha1", `filesha1("testdata/hello.txt")`, "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"},
		{"filesha256", `filesha256("testdata/hello.txt")`, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"filesha512", `filesha512("testdata/hello.txt")`, "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043"},
		{"filebase64sha256", `filebase64sha256("testdata/hello.txt")`, "LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="},
		{"filebase64sha512", `filebase64sha512("testdata/hello.txt")`, "m3HSJL1i83hdltRq0+o9czGb+8KJDKra4t/3JRlnPKcjI8PZm6XBHXx6zG4UuMXaDEZjR1wuXDre9G9zvN7AQw=="},
	}
	for _, tc := range fileHashTests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			val, err := ctx.EvaluateExpression(parseExpr(t, tc.expr))
			require.NoError(t, err)
			require.Equal(t, tc.expected, val.AsString())
		})
	}
}

// --- Function evaluation tests: non-deterministic / special ---

func TestFunctionEval_NonDeterministic(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)

	t.Run("timestamp", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `timestamp()`))
		require.NoError(t, err)
		require.Regexp(t, regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`), val.AsString())
	})

	t.Run("uuid", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `uuid()`))
		require.NoError(t, err)
		require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`), val.AsString())
	})

	t.Run("bcrypt", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `bcrypt("hello")`))
		require.NoError(t, err)
		require.True(t, len(val.AsString()) >= 59, "bcrypt hash should be at least 59 chars")
		require.Contains(t, val.AsString(), "$2a$")
	})

	t.Run("base64gzip", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `base64gzip("hello")`))
		require.NoError(t, err)
		// Gzip magic bytes (1f 8b) base64-encode to "H4sI" prefix.
		require.True(t, len(val.AsString()) > 0)
		// Verify roundtrip.
		require.Equal(t, "hello", evalStr(t, ctx, `base64gunzip(base64gzip("hello"))`))
	})

	t.Run("sensitive", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `sensitive("secret")`))
		require.NoError(t, err)
		require.True(t, val.IsMarked(), "sensitive value should be marked")
		unmarked, _ := val.Unmark()
		require.Equal(t, "secret", unmarked.AsString())
	})

	t.Run("nonsensitive", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `nonsensitive(sensitive("secret"))`))
		require.NoError(t, err)
		// nonsensitive removes the mark, but the value may still carry it in some implementations.
		unmarked, _ := val.Unmark()
		require.Equal(t, "secret", unmarked.AsString())
	})

	t.Run("type", func(t *testing.T) {
		t.Parallel()
		val, err := ctx.EvaluateExpression(parseExpr(t, `type("hello")`))
		require.NoError(t, err)
		// type() returns a capsule value wrapping a cty.Type, marked with TypeType.
		require.True(t, val.IsMarked())
		unmarked, _ := val.Unmark()
		require.True(t, unmarked.Type().IsCapsuleType(), "expected capsule type, got %s", unmarked.Type().FriendlyName())
	})
}

// --- Function evaluation tests: deprecated functions ---

func TestFunctionEval_Deprecated(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)

	tests := []struct {
		name string
		expr string
	}{
		{"list", `list("a", "b")`},
		{"map", `map("a", "1", "b", "2")`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ctx.EvaluateExpression(parseExpr(t, tc.expr))
			require.Error(t, err)
			require.Contains(t, err.Error(), "deprecated")
		})
	}
}
