package handlemessage

const StrainCollectionCallbackPrefix = "scf:"

// StrainCollectConfirm is returned after a successful collection button press.
type StrainCollectConfirm struct {
	Canonical      string
	ActorID        int64
	ReplyChatID    int64
	EncounterTotal int64
	Removed        bool   // decrement path (suppresses subscriber enqueue)
	FollowUpNotice string // optional plain text outbound after rebuilt card (e.g. additive 0→1 hint)
}
