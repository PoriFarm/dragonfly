package session

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// shownItem is a hotbar slot that the client sees as another item.
type shownItem struct {
	slot int
	t    protocol.ItemType
}

// ShowHotbarItem makes the hotbar slot look like an item of the type to this client, or like its real item
// again if it is nil. The whole inventory is sent in the latter case: the client may have moved the item
// meanwhile.
func (s *Session) ShowHotbarItem(slot int, t *protocol.ItemType) {
	if s == Nop || s.inv == nil {
		return
	}
	if t == nil {
		s.shown.Store(nil)
		s.sendInv(s.inv, protocol.WindowIDInventory)
		return
	}
	s.shown.Store(&shownItem{slot: slot, t: *t})
	real, _ := s.inv.Item(slot)
	s.sendItem(real, slot, protocol.WindowIDInventory)
}

// slotInstance is the stack in a slot of the window as the client should see it.
func (s *Session) slotInstance(it item.Stack, slot int, windowID uint32) protocol.ItemInstance {
	instance := instanceFromItem(s.br, it)
	if shown := s.shown.Load(); shown != nil && windowID == protocol.WindowIDInventory && shown.slot == slot {
		instance.Stack = showAs(instance.Stack, shown.t)
	}
	return instance
}

// showAs is the stack with another type. It is unbreakable: the client wears items it uses by itself, and
// would break this one.
func showAs(stack protocol.ItemStack, t protocol.ItemType) protocol.ItemStack {
	if stack.Count == 0 {
		return stack
	}
	data := make(map[string]any, len(stack.NBTData)+1)
	for k, v := range stack.NBTData {
		if k != "Damage" {
			data[k] = v
		}
	}
	data["Unbreakable"] = byte(1)
	stack.ItemType, stack.NBTData = t, data
	return stack
}
