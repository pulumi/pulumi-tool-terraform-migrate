package pkg

import (
	"encoding/json"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

func objectFromCty(val cty.Value) (map[string]interface{}, error) {
	bytes, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return nil, err
	}

	var m map[string]interface{}
	err = json.Unmarshal(bytes, &m)
	if err != nil {
		return nil, err
	}

	return m, nil
}
