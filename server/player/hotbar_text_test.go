package player

import "testing"

// Notices above the hotbar mark when they were sent, so a standing line (SendQuietPopup) can step
// aside for them; the quiet line itself never counts, and the time outlives the transaction.
func TestHotbarTextAt(t *testing.T) {
	data := &playerData{}
	p := &Player{playerData: data}
	p.SendQuietPopup("길라잡이")
	if !p.HotbarTextAt().IsZero() {
		t.Fatal("a quiet popup counted as a notice")
	}
	p.SendTip("채광 숙련도 +300 EXP")
	tip := (&Player{playerData: data}).HotbarTextAt()
	if tip.IsZero() {
		t.Fatal("a tip did not count")
	}
	p.SendQuietPopup("길라잡이")
	if p.HotbarTextAt() != tip {
		t.Fatal("a quiet popup moved the notice time")
	}
	for _, send := range []func(){func() { p.SendPopup("x") }, func() { p.SendJukeboxPopup("x") }} {
		data.hotbarTextAt = tip.Add(-1)
		send()
		if !p.HotbarTextAt().After(tip.Add(-1)) {
			t.Fatal("a popup did not count")
		}
	}
}
