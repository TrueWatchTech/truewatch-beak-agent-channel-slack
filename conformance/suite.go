package slackconformance

import (
	"path/filepath"
	"runtime"
	"testing"

	conformance "gitlab.jiagouyun.com/guance/beak-agent-channel-sdk/beak-channel-sdk-conformance"
)

// Run executes the Slack connector's reusable conformance suite. Keeping the
// fixture root relative to this source file lets the central release gate call
// the same real adapter without copying platform behavior into another repo.
func Run(t *testing.T) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Slack conformance fixture path")
	}
	fixtureRoot := filepath.Join(filepath.Dir(currentFile), "testdata", "beak-conformance")
	a := newAdapter()
	conformance.Run(t, conformance.Config{
		Platform:                 "slack",
		MetadataProvider:         a,
		CredentialSchemaProvider: a,
		CredentialValidator:      a,
		InboundParser:            a,
		Acknowledger:             a,
		CredentialCases: conformance.MustLoadJSON[[]conformance.CredentialValidationCase](
			t, filepath.Join(fixtureRoot, "credential_cases.json"),
		),
		InboundCases: conformance.MustLoadJSON[[]conformance.InboundCase](
			t, filepath.Join(fixtureRoot, "inbound_cases.json"),
		),
		AckCases: conformance.MustLoadJSON[[]conformance.AckCase](
			t, filepath.Join(fixtureRoot, "ack_cases.json"),
		),
	})
}
