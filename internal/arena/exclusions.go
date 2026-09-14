package arena

import "strings"

const paulSauronExclusionReason = "locally excluded as malfunctioning (paul-sauron bot family)"

// LocalExclusionReason reports bots that this client must not seat in Arena
// competitions. The exclusion is local; it does not change the server's bot
// state.
func LocalExclusionReason(name string) (string, bool) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "paul-sauron") {
		return paulSauronExclusionReason, true
	}
	return "", false
}
