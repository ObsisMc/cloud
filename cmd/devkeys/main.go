// devkeys generates the local key material the development Gateway and Cloud
// share: two purpose-separated Ed25519 pairs and the PKCE derivation key.
//
// Production keys are provisioned by the deployment; this command exists so
// `task dev` can start the real Gateway on a laptop without a human running
// openssl. It never overwrites an existing file, never prints private
// material, and writes only under .local/, which is ignored by Git.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", filepath.Join(".local", "gateway"), "directory that configs/config.yaml and configs/gateway.yaml point at")
	flag.Parse()
	if e := os.MkdirAll(*dir, 0o700); e != nil {
		return e
	}
	for _, name := range []string{"gateway-service", "user-identity"} {
		created, e := writeKeyPair(filepath.Join(*dir, name+".key"), filepath.Join(*dir, name+".pem"))
		if e != nil {
			return e
		}
		report(name+" key pair", created)
	}
	pkce := make([]byte, 32)
	if _, e := rand.Read(pkce); e != nil {
		return e
	}
	created, e := writeNew(filepath.Join(*dir, "gateway-pkce.key"), pkce)
	if e != nil {
		return e
	}
	report("PKCE key", created)
	secret := filepath.Join(*dir, "github-client-secret")
	if _, e := os.Stat(secret); errors.Is(e, fs.ErrNotExist) {
		fmt.Printf("missing %s: put your GitHub OAuth App client secret in it and export GATEWAY_GITHUB_CLIENT_ID\n", secret)
	}
	return nil
}

func report(what string, created bool) {
	if created {
		fmt.Printf("generated %s\n", what)
		return
	}
	fmt.Printf("kept existing %s\n", what)
}

// writeKeyPair stores a PKCS#8 private key for the Gateway and the matching
// PKIX public key for Cloud. Both files are created together or not at all so
// the pair never drifts.
func writeKeyPair(privatePath, publicPath string) (bool, error) {
	if exists(privatePath) || exists(publicPath) {
		if exists(privatePath) != exists(publicPath) {
			return false, fmt.Errorf("%s and %s must exist together; remove both to regenerate", privatePath, publicPath)
		}
		return false, nil
	}
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return false, e
	}
	privDER, e := x509.MarshalPKCS8PrivateKey(priv)
	if e != nil {
		return false, e
	}
	pubDER, e := x509.MarshalPKIXPublicKey(pub)
	if e != nil {
		return false, e
	}
	if _, e = writeNew(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})); e != nil {
		return false, e
	}
	if _, e = writeNew(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})); e != nil {
		return false, e
	}
	return true, nil
}

// writeNew creates the file owner-readable only and reports false when it
// already exists, so a rerun never replaces keys that a running Cloud trusts.
func writeNew(path string, content []byte) (bool, error) {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(e, fs.ErrExist) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	_, writeErr := f.Write(content)
	return true, errors.Join(writeErr, f.Close())
}

func exists(path string) bool {
	_, e := os.Stat(path)
	return e == nil
}
