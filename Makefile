.PHONY: build-deps build app package check clean

build-deps:
	./scripts/build-libusb.sh

build:
	./scripts/build.sh

app:
	./scripts/build.sh --app

package:
	./scripts/build.sh --package

check:
	./scripts/check.sh

clean:
	rm -rf bin dist
