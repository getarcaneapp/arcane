import 'settings.just'

set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list generate

# Agent tunnel protobuf/gRPC code
proto:
    {{ just_executable() }} lint proto
    cd {{ tunnel_proto_dir }} && ../.bin/buf generate
