package player

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/session"
)

// PreviewArmour shows stack in armour slot (0 helmet … 3 boots) to the player alone, without
// changing the armour it wears. EndArmourPreview shows the real armour again.
func (p *Player) PreviewArmour(slot int, stack item.Stack) {
	if p.session() != session.Nop {
		p.session().PreviewArmour(slot, stack)
	}
}

// EndArmourPreview shows the player its real armour again after PreviewArmour.
func (p *Player) EndArmourPreview() {
	if p.session() != session.Nop {
		p.session().ResendArmour()
	}
}
