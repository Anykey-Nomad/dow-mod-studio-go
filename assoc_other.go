//go:build !windows

package main

func isModuleAssociated() (bool, string, error) { return false, "", nil }
func registerModuleAssociation() error          { return nil }
func unregisterModuleAssociation() error        { return nil }
