// Command ssofake is a loopback AWS IAM Identity Center for driving Assay's
// AWS sign-in by hand or from a script (task studio:dev:ssofake, Rodney QA). It
// serves the OIDC device-authorization endpoints and the portal on one port,
// plus /_ctl/approve, /_ctl/deny, /_ctl/expire and /_ctl/state. With --minio it
// hands out MinIO STS credentials (so a real read against a local MinIO works);
// without it, fixed fake ones. It talks to nothing but loopback and is never
// part of a release build.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"github.com/SandwichLabs/assay/internal/testsupport/ssofake"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:38999", "listen address (loopback only)")
	minio := flag.String("minio", "", "MinIO host:port; role credentials are minted with STS AssumeRole")
	key := flag.String("key", "minioadmin", "MinIO root key id (with --minio)")
	secret := flag.String("secret", "", "MinIO root secret (with --minio)")
	life := flag.Duration("cred-life", 6*time.Minute, "how long the portal says role credentials last (Assay renews 5 minutes before)")
	flag.Parse()
	if !strings.HasPrefix(*addr, "127.0.0.1:") && !strings.HasPrefix(*addr, "localhost:") {
		log.Fatal("ssofake only listens on loopback")
	}
	opts := []ssofake.Option{ssofake.WithControl(), ssofake.WithCredLife(*life)}
	if *minio != "" {
		opts = append(opts, ssofake.WithCredentials(func() ssofake.Creds { return mint(*minio, *key, *secret, *life) }))
	}
	fmt.Printf("ssofake: listening on http://%s (oidc and portal)\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, ssofake.New(opts...))) //nolint:gosec // loopback dev tool
}

// mint asks MinIO's STS for temporary credentials with the root keys.
func mint(ep, key, secret string, life time.Duration) ssofake.Creds {
	form := url.Values{"Action": {"AssumeRole"}, "Version": {"2011-06-15"}, "DurationSeconds": {"900"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, "http://"+ep+"/", strings.NewReader(form))
	if err != nil {
		log.Printf("ssofake: %v", err)
		return ssofake.Creds{}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sum := sha256.Sum256([]byte(form))
	if err := v4.NewSigner().SignHTTP(context.Background(), aws.Credentials{AccessKeyID: key, SecretAccessKey: secret}, req, hex.EncodeToString(sum[:]), "sts", "us-east-1", time.Now()); err != nil {
		log.Printf("ssofake: %v", err)
		return ssofake.Creds{}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("ssofake: STS: %v", err)
		return ssofake.Creds{}
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Credentials struct {
			AccessKeyID     string `xml:"AccessKeyId"`
			SecretAccessKey string
			SessionToken    string
		} `xml:"AssumeRoleResult>Credentials"`
	}
	if err := xml.Unmarshal(body, &out); err != nil || out.Credentials.AccessKeyID == "" {
		fmt.Fprintf(os.Stderr, "ssofake: STS answered %d\n", resp.StatusCode)
		return ssofake.Creds{}
	}
	c := out.Credentials
	return ssofake.Creds{KeyID: c.AccessKeyID, Secret: c.SecretAccessKey, Token: c.SessionToken, Expires: time.Now().Add(life)}
}
