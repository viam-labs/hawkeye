.PHONY: build-local build-module build-docker deploy-remote clean

HOST := piracer@piracer.local

# Commands to build, compress and upload the module to the registry.
# Usually invoked for uploading a locally-built module via the CLI:
#
#   viam module upload --version 0.0.1 --platform linux/arm64 .
#
# Or doing a cloud build where the repo gets cloned behind the scenes:
#
#   viam module build start

build-local:
	mkdir -p bin
	go build -o bin/run ./src

build-module: build-local
	tar -czf bin/module.tar.gz bin/run setup.sh

# Commands to build the module on a laptop and copy it to the pi in a format
# that's compatible with the pi's kernel. Helps speed up development by avoiding
# the need to constantly upload and version the module in the registry between
# iterations, or do cloud builds in general.
#
# The robot config needs to have the service and module manually added like:
# 
#   "services": [
#     {
#       "name": "generic-service",
#       "api": "rdk:service:generic",
#       "model": "viam:tennis:hawkeye",
#       "attributes": {
#         "camera_name": "camera-1"
#         "vision": "vision-1-detect-ball",
#         "servo_steering": "servo-1-steering",
#         "servo_motor": "servo-2-motor"
#       }
#     }
#   ],
#   "modules": [
#     {
#       "type": "local",
#       "name": "hawkeye",
#       "executable_path": "/home/piracer/bin/run"
#     }
#   ]
#
# Then DoCommand can be called on the service from the UI, or from the CLI:
# 
#   viam robot part run --robot <robot id> --part <part id> --data '{ "command": "start", "routines": { <routine>: <args> } }' viam.service.generic.v1.GenericService/DoCommand
#
# To avoid needing to provide the ssh password constantly:
#
#   ssh-keygen -t ed25519               # if you don't have id_ed25519 files in ~/.ssh/
#   ssh-copy-id piracer@piracer.local
#
# The viam-agent status on the PiRacer can then be checked with:
#
#   sudo systemctl status viam-agent

build-docker:
	mkdir -p bin
	docker run --rm --platform=linux/arm64 \
	  -v "$(PWD):/src" -w /src \
	  -v piracer-gocache:/root/.cache/go-build \
	  -v piracer-gomod:/go/pkg/mod \
	  -e CGO_ENABLED=1 \
	  golang:1.25-bookworm \
	  go build -o bin/run ./src

deploy-remote: build-docker
	ssh $(HOST) "sudo systemctl stop viam-agent"
	scp bin/run $(HOST):/home/piracer/bin/run
	ssh $(HOST) "sudo systemctl start viam-agent"

# Command to delete build artifacts.

clean:
	rm -rf bin
