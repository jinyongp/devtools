//go:build !windows

package main

func windowsInstallerEntry() bool { return false }

func windowsNetworkHelperEntry() bool { return false }
