package player

import "github.com/sandertv/gophertunnel/minecraft/protocol"

// ShowMainHandAs makes the item held in the current hotbar slot look like an item of the type to every
// client, the player's own included, while the inventory keeps the real stack. Only the type is replaced:
// the count, the name and the enchantments shown stay those of the real stack. It lasts until
// ShowRealMainHand and shows only while that slot is held.
func (p *Player) ShowMainHandAs(t protocol.ItemType) {
	p.shownHand, p.shownSlot = &t, *p.heldSlot
	p.showHand()
}

// ShowRealMainHand ends a ShowMainHandAs.
func (p *Player) ShowRealMainHand() {
	if p.shownHand == nil {
		return
	}
	p.shownHand = nil
	p.showHand()
}

func (p *Player) showHand() {
	p.session().ShowHotbarItem(int(p.shownSlot), p.shownHand)
	for _, v := range p.viewers() {
		v.ViewEntityItems(p)
	}
}

// ShownMainHand is the type the held item looks like by ShowMainHandAs, if any.
func (p *Player) ShownMainHand() (protocol.ItemType, bool) {
	if p.shownHand == nil || p.shownSlot != *p.heldSlot {
		return protocol.ItemType{}, false
	}
	return *p.shownHand, true
}

// HeldSlot is the hotbar slot held, 0-8.
func (p *Player) HeldSlot() int { return int(*p.heldSlot) }
