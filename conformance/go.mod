module github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/conformance

go 1.23

replace github.com/TrueWatchTech/truewatch-beak-agent-channel-slack => ../

replace gitlab.jiagouyun.com/guance/beak-agent-channel-sdk/beak-channel-sdk-conformance => github.com/GuanceCloud/beak-channel-sdk-conformance v0.0.42

require (
	github.com/TrueWatchTech/truewatch-beak-agent-channel-slack v0.0.0-00010101000000-000000000000
	gitlab.jiagouyun.com/guance/beak-agent-channel-sdk/beak-channel-sdk-conformance v0.0.0-00010101000000-000000000000
)
