package launchprofile

// DependencyKind describes catalog evidence that a machine is supporting
// content, not a standalone title. A runnable parent remains a game even when
// other sets reference it through romof or cloneof. An unknown set stays unknown.
func (c MAMECatalog) DependencyKind(setName string) string {
	name := normalizeMAMESet(setName)
	machine, ok := c.Machines[name]
	if !ok {
		return ""
	}
	if machine.IsBIOS {
		return "bios"
	}
	if machine.IsDevice {
		return "device"
	}
	if machine.Runnable {
		return ""
	}
	for _, consumer := range c.Machines {
		for _, dependency := range consumer.dependencyReferences() {
			if dependency == name && normalizeMAMESet(consumer.Name) != name {
				return "support"
			}
		}
	}
	return ""
}
