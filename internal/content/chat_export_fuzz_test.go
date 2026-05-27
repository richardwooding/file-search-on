package content

import "testing"

// FuzzParseChatExport exercises the three chat-export parsers and the
// shape sniffer against arbitrary bytes. None may panic; malformed
// input must degrade to an empty / partial collector.
func FuzzParseChatExport(f *testing.F) {
	f.Add([]byte(slackArrayFixture))
	f.Add([]byte(`{"messages":` + slackArrayFixture + `}`))
	f.Add([]byte(discordFixture))
	f.Add([]byte(signalNDJSON))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[{"ts":"x"}]`))
	f.Add([]byte(`{"envelope":}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(``))
	f.Add([]byte(`[{"envelope":{"timestamp":-1}}]`))
	f.Add([]byte(`{"guild":{},"channel":{},"messages":[{"timestamp":"bad"}]}`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = parseSlackExport(data)
		_, _, _ = parseDiscordExport(data)
		_ = parseSignalExport(data)
		_, _ = sniffJSONShape(data)
		// Discriminators must also never panic on arbitrary bytes.
		_ = (&slackExportType{}).MatchesContent(data)
		_ = (&discordExportType{}).MatchesContent(data)
		_ = (&signalExportType{}).MatchesContent(data)
	})
}
