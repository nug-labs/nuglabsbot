package utils

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const strainCollectionRelPath = "assets/strain_collection.yml"

// StrainCollectionCopy is loaded from assets/strain_collection.yml (flat string map keys).
type StrainCollectionCopy struct {
	PressIfFound     string
	EncounterLine    string
	CongratsOnce     string
	CongratsPlural   string
	CommunityButton  string
	CallbackRecorded string
	CallbackExpired  string
}

func (c StrainCollectionCopy) sanitized() StrainCollectionCopy {
	if strings.TrimSpace(c.PressIfFound) == "" {
		c.PressIfFound = "Press if you found this strain"
	}
	if strings.TrimSpace(c.EncounterLine) == "" {
		c.EncounterLine = "%d times"
	}
	if strings.TrimSpace(c.CongratsOnce) == "" {
		c.CongratsOnce = "Congratulations! You have found your strain once. We have added it to your collection."
	}
	if strings.TrimSpace(c.CongratsPlural) == "" {
		c.CongratsPlural = "Congratulations! You have found %d times. We have added it to your collection."
	}
	if strings.TrimSpace(c.CommunityButton) == "" {
		c.CommunityButton = "Community"
	}
	if strings.TrimSpace(c.CallbackRecorded) == "" {
		c.CallbackRecorded = "Added to your collection."
	}
	if strings.TrimSpace(c.CallbackExpired) == "" {
		c.CallbackExpired = "This confirmation link has expired or was already used."
	}
	return c
}

func CommunityInviteURL() string {
	return strings.TrimSpace(os.Getenv("COMMUNITY_URL"))
}

var strainCollCache struct {
	mu   sync.RWMutex
	copy StrainCollectionCopy
	ok   bool
}

func StrainCollectionMessages() StrainCollectionCopy {
	strainCollCache.mu.Lock()
	defer strainCollCache.mu.Unlock()
	if strainCollCache.ok {
		return strainCollCache.copy.sanitized()
	}

	path := filepath.Join(".", strainCollectionRelPath)
	data, err := ParseSimpleMapYAML(path)
	out := StrainCollectionCopy{}
	if err == nil && len(data) > 0 {
		out.PressIfFound = data["press_if_found"]
		out.EncounterLine = data["encounter_line"]
		out.CongratsOnce = data["congrat_once"]
		out.CongratsPlural = data["congrat_plural"]
		out.CommunityButton = data["community_button"]
		out.CallbackRecorded = data["callback_recorded"]
		out.CallbackExpired = data["callback_expired"]
		strainCollCache.copy = out
		strainCollCache.ok = true
		return out.sanitized()
	}

	strainCollCache.copy = out
	strainCollCache.ok = true
	return out.sanitized()
}
