# V2Ray statistics protocol

Protocol and generated bindings copied unmodified from SagerNet/sing-box
v1.12.12, `experimental/v2rayapi/{stats.proto,stats.pb.go,stats_grpc.pb.go}`.
Upstream: https://github.com/SagerNet/sing-box/tree/v1.12.12/experimental/v2rayapi
License: GPL-3.0-or-later; see LICENSE in this directory.

sing-box registers `v2ray.core.app.stats.command.StatsService` at runtime,
overriding the service descriptor in these generated bindings. The collector
therefore invokes that compatible service name explicitly.
