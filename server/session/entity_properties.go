package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// IntProperty defines one integer entity property, read on the client with query.property(Name).
type IntProperty struct {
	Name     string
	Min, Max int32
}

// PropertiedEntityType is an entity type with integer entity properties. The session sends their
// definitions when the player joins (SyncActorProperty); the values travel with the entity's spawn
// and state packets.
type PropertiedEntityType interface {
	IntProperties() []IntProperty
}

// PropertiedEntity is an entity whose values for its type's IntProperties, in the same order, are
// sent to viewers.
type PropertiedEntity interface {
	IntPropertyValues() []int32
}

// PlayerIntProperties are the integer entity properties of minecraft:player, set once before the
// server starts. Players carry their values with player.Player.SetIntPropertyValues.
var PlayerIntProperties []IntProperty

// PlayerPropertyData is the definition of PlayerIntProperties for the StartGame packet, the only
// place the client takes the player's property definitions from, or nil without any.
func PlayerPropertyData() map[string]any {
	if len(PlayerIntProperties) == 0 {
		return nil
	}
	return intPropertyData("minecraft:player", PlayerIntProperties)
}

// sendEntityProperties sends the property definitions of every registered entity type that has any.
func (s *Session) sendEntityProperties(w *world.World) {
	for _, t := range w.EntityRegistry().Types() {
		pt, ok := t.(PropertiedEntityType)
		if !ok {
			continue
		}
		s.writePacket(&packet.SyncActorProperty{PropertyData: intPropertyData(t.EncodeEntity(), pt.IntProperties())})
	}
}

// intPropertyData is the SyncActorProperty data defining the integer properties of entity type id.
func intPropertyData(id string, defs []IntProperty) map[string]any {
	props := make([]map[string]any, 0, len(defs))
	for _, p := range defs {
		props = append(props, map[string]any{"name": p.Name, "type": int32(0), "min": p.Min, "max": p.Max})
	}
	return map[string]any{"type": id, "properties": props}
}

// entityProperties returns the property values of e for its spawn and state packets.
func entityProperties(e world.Entity) protocol.EntityProperties {
	pe, ok := e.(PropertiedEntity)
	if !ok {
		return protocol.EntityProperties{}
	}
	values := pe.IntPropertyValues()
	props := protocol.EntityProperties{IntegerProperties: make([]protocol.IntegerEntityProperty, len(values))}
	for i, v := range values {
		props.IntegerProperties[i] = protocol.IntegerEntityProperty{Index: uint32(i), Value: v}
	}
	return props
}
