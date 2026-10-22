import 'settings.just'

set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list generate

# Edge tunnel protobuf/gRPC code
proto:
    {{ just_executable() }} lint proto
    cd {{ edge_proto_dir }} && ../.bin/buf generate
