package contract

// AddObservedEntity registers an entity once.
func (i *Investigation) AddObservedEntity(e Entity) {
	if i == nil || e.ID == "" {
		return
	}
	for _, existing := range i.ObservedEntities {
		if existing.Kind == e.Kind && existing.ID == e.ID && existing.ParentID == e.ParentID {
			return
		}
	}
	i.ObservedEntities = append(i.ObservedEntities, e)
}

// KnowsEntity returns true only for an entity previously emitted by Diagnos.
func (i *Investigation) KnowsEntity(e Entity) bool {
	if i == nil {
		return false
	}
	for _, existing := range i.ObservedEntities {
		if existing.Kind == e.Kind && existing.ID == e.ID && existing.ParentID == e.ParentID {
			return true
		}
	}
	return false
}
