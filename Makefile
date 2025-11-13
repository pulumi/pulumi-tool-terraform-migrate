.PHONY: all clean

all: bin/pulumi-terraform-migrate

bin/pulumi-terraform-migrate::
	@mkdir -p bin
	go build -o bin/pulumi-terraform-migrate ./cmd/pulumi-terraform-migrate

clean:
	rm -rf bin
