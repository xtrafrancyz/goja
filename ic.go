package goja

import "github.com/dop251/goja/unistring"

const maxInlinePropCacheEntries = 4
const icEnabled = true

type canBeUsedAsPrototype interface {
	markUsedAsPrototype()
}

func markObjectUsedAsPrototype(o *Object) {
	if o == nil {
		return
	}
	if p, ok := o.self.(canBeUsedAsPrototype); ok {
		p.markUsedAsPrototype()
	}
}

type icClassEntry struct {
	class *tinyClass
	index int
}

type icClassProtoEntry struct {
	class            *tinyClass
	proto            *Object
	uncacheableProto *Object
	value            Value
	protoEpoch       uint64
	isProperty       bool
}

type inlinePropCache struct {
	entries      []icClassEntry
	protoEntries []*icClassProtoEntry
	mega         bool
}

func canProtoBeCached(o *Object) bool {
	switch o.self.(type) {
	case *baseObject,
		*baseFuncObject,
		*baseJsFuncObject,
		*funcObject,
		*generatorFuncObject,
		*asyncFuncObject,
		*classFuncObject,
		*methodFuncObject,
		*generatorMethodFuncObject,
		*asyncMethodFuncObject,
		*arrowFuncObject,
		*asyncArrowFuncObject,
		*nativeFuncObject,
		*wrappedFuncObject,
		*boundFuncObject,
		*generatorObject:
		return true
	}
	return false
}

func (c *inlinePropCache) learn(entry icClassEntry) {
	if len(c.entries) < maxInlinePropCacheEntries {
		c.entries = append(c.entries, entry)
	} else {
		c.mega = true
		c.entries = nil
		c.protoEntries = nil
	}
}

func (c *inlinePropCache) learnProto(entry *icClassProtoEntry) {
	for i, e := range c.protoEntries {
		if e.class == entry.class && e.proto == entry.proto {
			stats.incICProtoEpochUpdateCount()
			c.protoEntries[i] = entry
			return
		}
	}
	if len(c.protoEntries) < maxInlinePropCacheEntries {
		c.protoEntries = append(c.protoEntries, entry)
	} else {
		c.mega = true
		c.entries = nil
		c.protoEntries = nil
	}
}

func (c *inlinePropCache) getStr(r *Runtime, o *Object, prop unistring.String, recv Value) Value {
	if !icEnabled {
		return o.self.getStr(prop, recv)
	}
	if c.mega {
		stats.incICMegaCount()
		return o.self.getStr(prop, recv)
	}
	if tiny, ok := o.self.(*tinyObject); ok {
		for _, e := range c.entries {
			if e.class == tiny.class {
				stats.incICHitCount()
				if e.index < 0 {
					return nil
				}
				return tiny.values[e.index]
			}
		}

		for _, e := range c.protoEntries {
			if e.class == tiny.class && e.proto == tiny.prototype && e.protoEpoch == r.protoEpoch {
				if e.uncacheableProto != nil {
					stats.incICProtoUncacheableHitCount()
					return e.uncacheableProto.self.getStr(prop, recv)
				}
				stats.incICProtoHitCount()
				if e.isProperty {
					return e.value.(*valueProperty).get(recv)
				}
				return e.value
			}
		}
		stats.incICMissCount()

		// own property on object
		if idx := tiny.class.idxForName(prop); idx >= 0 {
			c.learn(icClassEntry{
				class: tiny.class,
				index: idx,
			})
			return tiny.values[idx]
		}

		// own property on prototype
		proto := tiny.prototype
		for proto != nil {
			// instead of traversing the prototype chain, we can cache first uncacheable prototype and delegate to it,
			// since we know that the property is not on any of the prototypes before it
			if !canProtoBeCached(proto) {
				c.learnProto(&icClassProtoEntry{
					class:            tiny.class,
					proto:            proto,
					uncacheableProto: proto,
					protoEpoch:       r.protoEpoch,
					value:            _undefined,
				})
				return proto.self.getStr(prop, recv)
			}

			val := proto.self.getOwnPropStr(prop)
			if val != nil {
				_, isProperty := val.(*valueProperty)
				c.learnProto(&icClassProtoEntry{
					class:      tiny.class,
					proto:      tiny.prototype,
					protoEpoch: r.protoEpoch,
					value:      val,
					isProperty: isProperty,
				})
				if isProperty {
					return val.(*valueProperty).get(recv)
				}
				return val
			}
			proto = proto.self.proto()
		}

		// absent
		c.learnProto(&icClassProtoEntry{
			class:      tiny.class,
			proto:      tiny.prototype,
			protoEpoch: r.protoEpoch,
			value:      nil,
		})
		return nil
	}

	return o.self.getStr(prop, recv)
}

type icClassPutEntry struct {
	class      *tinyClass
	transition *tinyClass
	index      int
	deoptimize bool
}

// Special case for putProp where we don't care about the proto chain
type inlinePutPropCache struct {
	entries []icClassPutEntry
	mega    bool
}

func (c *inlinePutPropCache) learn(entry icClassPutEntry) {
	if len(c.entries) < maxInlinePropCacheEntries {
		c.entries = append(c.entries, entry)
	} else {
		c.mega = true
		c.entries = nil
	}
}

func (c *inlinePutPropCache) _putProp(o *Object, name unistring.String, val Value) {
	if !icEnabled {
		o.self._putProp(name, val, true, true, true)
		return
	}
	if c.mega {
		stats.incICMegaCount()
		o.self._putProp(name, val, true, true, true)
		return
	}

	if tiny, ok := o.self.(*tinyObject); ok {
		for _, e := range c.entries {
			if e.class == tiny.class {
				stats.incICHitCount()
				if e.transition != nil {
					tiny.class = e.transition
					tiny.values = append(tiny.values, val)
				} else if e.index >= 0 {
					tiny.values[e.index] = val
				} else if e.deoptimize {
					tiny.deoptimize()._put(name, val)
				}
				return
			}
		}

		// existing key
		if idx := tiny.class.idxForName(name); idx >= 0 {
			c.learn(icClassPutEntry{
				class: tiny.class,
				index: idx,
			})
			tiny.values[idx] = val
			return
		}

		classBefore := tiny.class
		if tiny._addProp(name, val, false) {
			// deoptimized
			if tiny.val.self != tiny {
				c.learn(icClassPutEntry{
					class:      classBefore,
					deoptimize: true,
					index:      -1,
				})
				return
			}
			// transitioned to a new class
			if classBefore != tiny.class {
				c.learn(icClassPutEntry{
					class:      classBefore,
					transition: tiny.class,
					index:      -1,
				})
				return
			}
		}
	}

	o.self._putProp(name, val, true, true, true)
}

type icClassSetEntry struct {
	class         *tinyClass
	transition    *tinyClass
	protoProperty *valueProperty
	proto         *Object
	slowPath      bool
	protoEpoch    uint64
	index         int
	deoptimize    bool
}

// Special case for putProp where we don't care about the proto chain
type inlineSetPropCache struct {
	entries []icClassSetEntry
	mega    bool
}

func (c *inlineSetPropCache) learn(entry icClassSetEntry) {
	for i, e := range c.entries {
		if e.class == entry.class && e.proto == entry.proto {
			stats.incICProtoEpochUpdateCount()
			c.entries[i] = entry
			return
		}
	}
	if len(c.entries) < maxInlinePropCacheEntries {
		c.entries = append(c.entries, entry)
	} else {
		c.mega = true
		c.entries = nil
	}
}

func (c *inlineSetPropCache) findProtoDefinedProp(o *Object, name unistring.String) *valueProperty {
	proto := o
	for proto != nil {
		if prop := proto.self.getOwnPropStr(name); prop != nil {
			if p, ok := prop.(*valueProperty); ok {
				return p
			}
		}
		proto = proto.self.proto()
	}
	return nil
}

func (c *inlineSetPropCache) setProp(r *Runtime, o *Object, name unistring.String, val Value, throw bool) {
	if !icEnabled {
		o.self.setOwnStr(name, val, throw)
		return
	}
	if c.mega {
		stats.incICMegaCount()
		o.self.setOwnStr(name, val, throw)
		return
	}

	if tiny, ok := o.self.(*tinyObject); ok {
		for _, e := range c.entries {
			if e.class == tiny.class {
				if e.index >= 0 {
					stats.incICHitCount()
					tiny.values[e.index] = val
					return
				}
				if e.proto == tiny.prototype && e.protoEpoch == r.protoEpoch {
					stats.incICHitCount()
					if e.transition != nil {
						tiny.class = e.transition
						tiny.values = append(tiny.values, val)
					} else if e.deoptimize {
						tiny.deoptimize()._put(name, val)
					} else if e.protoProperty != nil {
						e.protoProperty.set(o, val)
					} else if e.slowPath {
						stats.incICProtoUncacheableHitCount()
						o.self.setOwnStr(name, val, throw)
					} else if throw {
						r.typeErrorResult(throw, "Cannot assign to read only property '%s'", name)
					}
					return
				}
			}
		}

		stats.incICMissCount()

		// existing key
		if idx := tiny.class.idxForName(name); idx >= 0 {
			c.learn(icClassSetEntry{
				class: tiny.class,
				index: idx,
			})
			tiny.values[idx] = val
			return
		}

		// check proto chain for a property defined on the prototype
		proto := o
		for proto != nil {
			if !canProtoBeCached(proto) {
				// uncacheable prototype, we can't cache it, but we can still call setStr on it
				c.learn(icClassSetEntry{
					class:      tiny.class,
					proto:      tiny.prototype,
					protoEpoch: r.protoEpoch,
					slowPath:   true,
					index:      -1,
				})
				o.self.setOwnStr(name, val, throw)
				return
			}
			if propValue := proto.self.getOwnPropStr(name); propValue != nil {
				if prop, ok := propValue.(*valueProperty); ok {
					if !prop.isWritable() {
						// no-op for non-writable properties
						c.learn(icClassSetEntry{
							class:      tiny.class,
							proto:      tiny.prototype,
							protoEpoch: r.protoEpoch,
							index:      -1,
						})
						r.typeErrorResult(throw, "Cannot assign to read only property '%s'", name)
						return
					} else if prop.setterFunc != nil {
						// setter function, call it
						c.learn(icClassSetEntry{
							class:         tiny.class,
							proto:         tiny.prototype,
							protoEpoch:    r.protoEpoch,
							index:         -1,
							protoProperty: prop,
						})
						prop.set(o, val)
						return
					}
					// nothing interesting to cache, pass through
					break
				}
			}
			proto = proto.self.proto()
		}

		classBefore := tiny.class
		if tiny._addProp(name, val, throw) {
			// deoptimized
			if tiny.val.self != tiny {
				c.learn(icClassSetEntry{
					class:      classBefore,
					proto:      tiny.prototype,
					protoEpoch: r.protoEpoch,
					deoptimize: true,
					index:      -1,
				})
				return
			}
			// transitioned to a new class
			if classBefore != tiny.class {
				c.learn(icClassSetEntry{
					class:      classBefore,
					proto:      tiny.prototype,
					protoEpoch: r.protoEpoch,
					transition: tiny.class,
					index:      -1,
				})
				return
			}
		}
	}

	o.self.setOwnStr(name, val, throw)
}

func (c *inlineSetPropCache) setPropRecv(r *Runtime, o *Object, name unistring.String, val, receiver Value, throw bool) {
	if !icEnabled {
		o.setStr(name, val, receiver, throw)
		return
	}

	if o == receiver {
		c.setProp(r, o, name, val, throw)
		return
	}

	o.setStr(name, val, receiver, throw)
}
