package main

import "github.com/yellowdking/GravitySSH/internal/sshclient"

func (a *App) GetSSHCacheInfo() (sshclient.SSHCacheInfo, error) {
	if err := a.ensureReady(); err != nil {
		return sshclient.SSHCacheInfo{}, err
	}
	return a.sshMgr.GetSSHCacheInfo(), nil
}

func (a *App) ClearSSHHostCache(host string, port int) (sshclient.SSHCacheClearResult, error) {
	if err := a.ensureReady(); err != nil {
		return sshclient.SSHCacheClearResult{}, err
	}
	return a.sshMgr.ClearSSHHostCache(host, port)
}

func (a *App) ClearAllSSHCache() (sshclient.SSHCacheClearResult, error) {
	if err := a.ensureReady(); err != nil {
		return sshclient.SSHCacheClearResult{}, err
	}
	return a.sshMgr.ClearAllSSHCache()
}
