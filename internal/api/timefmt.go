package api

import (
	"fmt"
	"time"
)

// humanUntil formate le délai jusqu'à t en français court ("dans 2 j 03 h",
// "dans 45 min", "imminent"). Utilisé par la bannière d'exam du classement.
func humanUntil(t time.Time) string {
	d := time.Until(t)
	if d <= 0 {
		return "imminent"
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	switch {
	case mins >= 24*60:
		return fmt.Sprintf("dans %d j %02d h", mins/(24*60), (mins%(24*60))/60)
	case mins >= 60:
		return fmt.Sprintf("dans %d h %02d min", mins/60, mins%60)
	case mins >= 1:
		return fmt.Sprintf("dans %d min", mins)
	default:
		return "dans moins d'une minute"
	}
}
