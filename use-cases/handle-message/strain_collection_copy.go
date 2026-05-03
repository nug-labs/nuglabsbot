package handlemessage

import (
	"path/filepath"
	"strings"
	"sync"

	"nuglabsbot-v2/utils"
)

const systemOutgoingYmlRel = "assets/outgoing-messages/system-messages.yml"

// StrainCollectionCopy holds strings from assets/outgoing-messages/system-messages.yml (cards, callbacks, shared replies).
type StrainCollectionCopy struct {
	PressIfFound                       string
	EncounterLine                      string
	EncounterLineSingular              string // unused when encounter count != 1; one %d
	CommunityButton                    string
	CallbackRecorded                   string
	CallbackExpired                    string
	CallbackRemovedOne                 string // one %s: strain display/canonical
	EncounterAdditiveZeroToOneFollowUp string // first-encounter (0→1) follow-up DM plain text

	StrainSearchDisabled               string
	StrainSearchTemporarilyUnavailable string
	StrainPleaseProvideName            string
	StrainNoMatching                   string
	UnknownQueryFallback               string

	SubscriptionEnabled  string
	SubscriptionDisabled string

	URLInvalid                string
	URLDomainNotWhitelisted   string
	URLUnreadableBody         string
	URLNoStrainCandidates     string
	URLNoKnownStrains         string
	URLStrainsNotFoundHeading string
}

func (c StrainCollectionCopy) sanitized() StrainCollectionCopy {
	if strings.TrimSpace(c.PressIfFound) == "" {
		c.PressIfFound = "Press if you found this strain"
	}
	if strings.TrimSpace(c.EncounterLine) == "" {
		c.EncounterLine = "%d times"
	}
	if strings.TrimSpace(c.EncounterLineSingular) == "" {
		c.EncounterLineSingular = "%d time"
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
	if strings.TrimSpace(c.CallbackRemovedOne) == "" {
		c.CallbackRemovedOne = "Removed one instance of %s from your collection."
	}
	if strings.TrimSpace(c.EncounterAdditiveZeroToOneFollowUp) == "" {
		c.EncounterAdditiveZeroToOneFollowUp = "First encounter logged. To withdraw please tap the original button again."
	}

	if strings.TrimSpace(c.StrainSearchDisabled) == "" {
		c.StrainSearchDisabled = "Strain search is unavailable right now."
	}
	if strings.TrimSpace(c.StrainSearchTemporarilyUnavailable) == "" {
		c.StrainSearchTemporarilyUnavailable = "Strain search is temporarily unavailable."
	}
	if strings.TrimSpace(c.StrainPleaseProvideName) == "" {
		c.StrainPleaseProvideName = "Please provide a strain name."
	}
	if strings.TrimSpace(c.StrainNoMatching) == "" {
		c.StrainNoMatching = "No matching strain found."
	}
	if strings.TrimSpace(c.UnknownQueryFallback) == "" {
		c.UnknownQueryFallback = "Unknown query"
	}

	if strings.TrimSpace(c.SubscriptionEnabled) == "" {
		c.SubscriptionEnabled = "Subscription enabled."
	}
	if strings.TrimSpace(c.SubscriptionDisabled) == "" {
		c.SubscriptionDisabled = "Subscription disabled."
	}

	if strings.TrimSpace(c.URLInvalid) == "" {
		c.URLInvalid = "Please send a valid URL."
	}
	if strings.TrimSpace(c.URLDomainNotWhitelisted) == "" {
		c.URLDomainNotWhitelisted = "URL domain is not whitelisted."
	}
	if strings.TrimSpace(c.URLUnreadableBody) == "" {
		c.URLUnreadableBody = "Unable to extract readable body content from URL."
	}
	if strings.TrimSpace(c.URLNoStrainCandidates) == "" {
		c.URLNoStrainCandidates = "No strain names could be extracted."
	}
	if strings.TrimSpace(c.URLNoKnownStrains) == "" {
		c.URLNoKnownStrains = "No known strains found from URL content."
	}
	if strings.TrimSpace(c.URLStrainsNotFoundHeading) == "" {
		c.URLStrainsNotFoundHeading = "Not found:"
	}

	return c
}

var systemOutgoingCache struct {
	mu  sync.RWMutex
	doc StrainCollectionCopy
	ok  bool
}

// StrainCollectionMessages returns cached copy from assets/outgoing-messages/system-messages.yml (loaded once per process).
func StrainCollectionMessages() StrainCollectionCopy {
	systemOutgoingCache.mu.Lock()
	defer systemOutgoingCache.mu.Unlock()
	if systemOutgoingCache.ok {
		return systemOutgoingCache.doc.sanitized()
	}

	path := filepath.Join(".", systemOutgoingYmlRel)
	data, err := utils.ParseSimpleMapYAML(path)
	out := StrainCollectionCopy{}
	if err == nil && len(data) > 0 {
		out.PressIfFound = data["press_if_found"]
		out.EncounterLine = data["encounter_line"]
		out.EncounterLineSingular = data["encounter_line_singular"]
		out.CommunityButton = data["community_button"]
		out.CallbackRecorded = data["callback_recorded"]
		out.CallbackExpired = data["callback_expired"]
		out.CallbackRemovedOne = data["callback_removed_one"]
		out.EncounterAdditiveZeroToOneFollowUp = data["encounter_additive_zero_to_one_followup"]
		out.StrainSearchDisabled = data["strain_search_disabled"]
		out.StrainSearchTemporarilyUnavailable = data["strain_search_temporarily_unavailable"]
		out.StrainPleaseProvideName = data["strain_please_provide_name"]
		out.StrainNoMatching = data["strain_no_matching"]
		out.UnknownQueryFallback = data["unknown_query_fallback"]
		out.SubscriptionEnabled = data["subscription_enabled"]
		out.SubscriptionDisabled = data["subscription_disabled"]
		out.URLInvalid = data["url_invalid"]
		out.URLDomainNotWhitelisted = data["url_domain_not_whitelisted"]
		out.URLUnreadableBody = data["url_unreadable_body"]
		out.URLNoStrainCandidates = data["url_no_strain_candidates"]
		out.URLNoKnownStrains = data["url_no_known_strains"]
		out.URLStrainsNotFoundHeading = data["url_strains_not_found_heading"]
		systemOutgoingCache.doc = out
		systemOutgoingCache.ok = true
		return out.sanitized()
	}

	systemOutgoingCache.doc = out
	systemOutgoingCache.ok = true
	return out.sanitized()
}
