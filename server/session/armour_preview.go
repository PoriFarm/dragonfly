package session

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// PreviewArmour shows stack in armour slot (0 helmet … 3 boots) to this viewer alone: only the
// client's own armour window changes, so the server inventory and other viewers keep the real
// armour. ResendArmour takes it back.
func (s *Session) PreviewArmour(slot int, stack item.Stack) {
	if slot < 0 || slot > 3 {
		return
	}
	s.sendItem(stack, slot, protocol.WindowIDArmour)
}

// ResendArmour sends the real armour again, ending a PreviewArmour.
func (s *Session) ResendArmour() {
	if s.armour == nil {
		return
	}
	s.sendInv(s.armour.Inventory(), protocol.WindowIDArmour)
}
