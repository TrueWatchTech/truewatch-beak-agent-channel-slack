package slackconformance

import (
	"context"
	"testing"

	conformance "gitlab.jiagouyun.com/guance/beak-agent-channel-sdk/beak-channel-sdk-conformance"
)

func TestConformance(t *testing.T) {
	Run(t)
}

func TestSlackThreadIDConformanceCanary(t *testing.T) {
	a := newAdapter()
	cases := conformance.MustLoadJSON[[]conformance.InboundCase](t, "testdata/beak-conformance/inbound_cases.json")
	var checked bool
	for _, tc := range cases {
		if tc.Expect.ThreadID == "" {
			continue
		}
		checked = true
		got, err := a.ParseInbound(context.Background(), tc.Fixture)
		conformance.AssertInboundMessages(t, "slack", got, err, tc.Expect)
	}
	if !checked {
		t.Fatal("expected at least one Slack inbound conformance case to assert thread_id")
	}
}
