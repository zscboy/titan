package tunnel

import "strings"

const netErrCloseByRemoteHost = "closed by the remote host"

func isNetErrCloseByRemoteHost(err error) bool {
	if strings.Contains(err.Error(), netErrCloseByRemoteHost) {
		return true
	}
	return false
}
