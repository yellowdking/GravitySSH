package main

import "github.com/yellowdking/GravitySSH/internal/sshclient"

func (a *App) GetSSHCacheInfo() (sshclient.SSHCacheInfo, error) {
	_, sshMgr, _, err := a.readyDeps()
	if err != nil {
		return sshclient.SSHCacheInfo{}, err
	}
	return sshMgr.GetSSHCacheInfo(), nil
}

func (a *App) ClearSSHHostCache(host string, port int) (sshclient.SSHCacheClearResult, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return sshclient.SSHCacheClearResult{}, err
	}
	return sshMgr.ClearSSHHostCache(host, port)
}

func (a *App) ClearAllSSHCache() (sshclient.SSHCacheClearResult, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return sshclient.SSHCacheClearResult{}, err
	}
	return sshMgr.ClearAllSSHCache()
}
