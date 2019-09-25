package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/cloudflare/certinel"
	"github.com/cloudflare/certinel/fswatcher"
	whhttp "github.com/slok/kubewebhook/pkg/http"
	"github.com/slok/kubewebhook/pkg/log"
	"github.com/slok/kubewebhook/pkg/webhook/mutating"
	v1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
)

const defaultDomain = "superhub.io"

// sorted string slice impl
type byLength []string

func (s byLength) Len() int {
	return len(s)
}
func (s byLength) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byLength) Less(i, j int) bool {
	return len(s[i]) < len(s[j])
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz"

func randStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

type nothing struct{}

func main() {
	logger := &log.Std{Debug: true}
	reg, err := regexp.Compile("[^a-zA-Z0-9]+")
	if err != nil {
		logger.Errorf("%v", err)
	}

	mt := mutating.MutatorFunc(func(_ context.Context, obj metav1.Object) (bool, error) {
		ingress := obj.(*v1beta1.Ingress)

		spec := &ingress.Spec
		rulesHosts := make(map[string]nothing)
		tlsHosts := make(map[string]nothing)

		// look before we leap again
		if spec.Rules == nil {
			spec.Rules = make([]v1beta1.IngressRule, 0)
		}

		for _, r := range spec.Rules {
			rulesHosts[r.Host] = nothing{}
		}

		if len(rulesHosts) > 0 {

			// look before we leap again
			if spec.TLS == nil {
				spec.TLS = make([]v1beta1.IngressTLS, 0)
			}
			for i := range spec.TLS {
				for _, h := range spec.TLS[i].Hosts {
					tlsHosts[h] = nothing{}
				}
			}

			// get the diff of rules vs all IngressTLS objects
			diff := make([]string, 0)
			for k, _ := range rulesHosts {
				if _, exists := tlsHosts[k]; !exists {
					logger.Debugf("found unmatched host: %s", k)
					diff = append(diff, k)
				}
			}

			if len(diff) > 0 {
				// there is a 63 char limit in the CN of cert-manager/LE
				// so we sort the slice of domain names so the shortest is first
				// if it is over 63 characters, we'll need to synthesize a new one and make it first
				sort.Sort(byLength(diff))
				if len(diff[0]) > 63 {
					prefix := reg.ReplaceAllString(diff[0][0:6], "")
					randstr := randStringBytes(6)
					dns1 := fmt.Sprintf("%s.%s.%s", prefix, randstr, defaultDomain)
					diff = append([]string{dns1}, diff...)
				}
				// create the IngressTLS Object with our extra hosts and a custom secret
				sekret := fmt.Sprintf("auto-%s-tls", ingress.Name)
				newtls := v1beta1.IngressTLS{
					Hosts:      diff,
					SecretName: sekret,
				}
				spec.TLS = append(spec.TLS, newtls)
				logger.Debugf("appending tls block: %v", newtls)
			} else {
				logger.Debugf("no diffs found. No changes needed.")

			}
		}

		return false, nil
	})

	cfg := mutating.WebhookConfig{
		Name: "tls-host-controller",
		Obj:  &v1beta1.Ingress{},
	}

	wh, err := mutating.NewWebhook(cfg, mt, nil, nil, logger)
	if err != nil {
		panic(err)
	}

	// Get the handler for our webhook.
	whHandler, err := whhttp.HandlerFor(wh)
	if err != nil {
		panic(err)
	}

	watcher, err := fswatcher.New("/data/tls.crt", "/data/tls.key")
	if err != nil {
		logger.Errorf("fatal: unable to read server certificate. err='%s'", err)
	}
	sentinel := certinel.New(watcher, func(err error) {
		logger.Infof("error: certinel was unable to reload the certificate. err='%s'", err)
	})

	sentinel.Watch()

	server := http.Server{
		Addr:    ":4443",
		Handler: whHandler,
		TLSConfig: &tls.Config{
			GetCertificate: sentinel.GetCertificate,
		},
	}

	logger.Infof("Listening on :4443")
	err = server.ListenAndServeTLS("", "")

	if err != nil {
		panic(err)
	}
}
