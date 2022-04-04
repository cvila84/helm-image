package credentials

import (
	"github.com/danieljoos/wincred"
)

func GetAuthFromVault(repo string) (string, string, error) {
	cred, err := wincred.GetGenericCredential(repo)
	if err == nil {
		return cred.UserName, string(cred.CredentialBlob), nil
	} else {
		return "", "", err
	}
}
