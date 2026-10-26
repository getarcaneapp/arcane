set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list bench

# Agent tunnel transports with allocation counts
tunnel count="3" benchtime="2s":
    cd backend && go test -run '^$' -bench '^BenchmarkAgentTunnelProxyRequest$' -benchmem -count={{ count }} -benchtime={{ benchtime }} ./pkg/libarcane/agent

# Agent tunnel transports with a memory profile
memory profile="agent_tunnel.mem.out" benchtime="5s":
    cd backend && go test -run '^$' -bench '^BenchmarkAgentTunnelProxyRequest$' -benchmem -benchtime={{ benchtime }} -memprofile={{ profile }} ./pkg/libarcane/agent
