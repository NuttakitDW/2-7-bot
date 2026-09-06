package onyx

import "github.com/nuttakit/2-7-bot/internal/handclass"

// Experiments replace this table at build time with independently priced
// additions to the original opening range: 0 folds, 1 calls, 2 raises.
// The priced profile refuses an empty table; default profiles do not use it.
var pricedOpening [handclass.Num]uint8
