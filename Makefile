GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOOS?=windows
GOARCH?=amd64
GOTARGETENV=GOOS=$(GOOS) GOARCH=$(GOARCH)
# The HWiNFO shared-memory bridge is cgo; cross-compiling from Linux/WSL needs mingw-w64.
WINCC?=x86_64-w64-mingw32-gcc
CGOWINENV=CGO_ENABLED=1 CC=$(WINCC)

SDPLUGINDIR=./com.exension.hwinfo.sdPlugin

PROTOS=$(wildcard ./*/**/**/*.proto)
PROTOPB=$(PROTOS:.proto=.pb.go)

# The plugin ships hwinfo.exe + hwinfo-bridge.exe; Windows only.
plugin:
	$(GOTARGETENV) $(GOBUILD) -o $(SDPLUGINDIR)/hwinfo.exe ./cmd/hwinfo_streamdeck_plugin
	$(GOTARGETENV) $(CGOWINENV) $(GOBUILD) -o $(SDPLUGINDIR)/hwinfo-bridge.exe ./cmd/hwinfo-bridge
	-@install-plugin.bat

proto: $(PROTOPB)

$(PROTOPB): $(PROTOS)
	.cache/protoc/bin/protoc \
 		--go_out=Mgrpc/service_config/service_config.proto=/internal/proto/grpc_service_config:. \
		--go-grpc_out=Mgrpc/service_config/service_config.proto=/internal/proto/grpc_service_config:. \
  		--go_opt=paths=source_relative \
		--go-grpc_opt=paths=source_relative \
		$(<)

# plugin:
# 	-@kill-streamdeck.bat
# 	@xcopy com.exension.hwinfo.sdPlugin $(APPDATA)\\Elgato\\StreamDeck\\Plugins\\com.exension.hwinfo.sdPlugin\\ /E /Q /Y
# 	@start-streamdeck.bat

debug:
	$(GOTARGETENV) $(GOBUILD) -o $(SDPLUGINDIR)/hwinfo.exe ./cmd/hwinfo_debugger
	-@install-plugin.bat
# @xcopy com.exension.hwinfo.sdPlugin $(APPDATA)\\Elgato\\StreamDeck\\Plugins\\com.exension.hwinfo.sdPlugin\\ /E /Q /Y

verify:
	$(GOTARGETENV) $(CGOWINENV) $(GOCMD) build ./...
	$(GOCMD) test $$($(GOCMD) list ./... 2>/dev/null | grep -v 'cmd/hwinfo_streamdeck_plugin\|cmd/hwinfo_debugger\|app/hwinfostreamdeckplugin')
	bash scripts/verify-settings-pi.sh
	streamdeck validate $(SDPLUGINDIR)

release: verify plugin
	-@rm build/com.exension.hwinfo.streamDeckPlugin
	@rm -f $(SDPLUGINDIR)/hwinfo $(SDPLUGINDIR)/mock-bridge
	streamdeck pack com.exension.hwinfo.sdPlugin --output build --force

# Version bumps are explicit. Commit/release paths must not mutate manifest.json.
bump-version:
	./scripts/bump-manifest-version.sh
