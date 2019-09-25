package main

import (
	"bytes"
	"context"
	whhttp "github.com/slok/kubewebhook/pkg/http"
	"github.com/slok/kubewebhook/pkg/log"
	"github.com/slok/kubewebhook/pkg/webhook/mutating"
	v1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"text/template"
)

type nothing struct{}

func main() {
	logger := &log.Std{Debug: true}

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
				// create the IngressTLS Object with our extra hosts and a custom secret
				var sekret bytes.Buffer
				tmpl, err := template.New("secret").Parse("auto-{{ .Name }}-tls-secret")
				if err != nil {
					panic(err)
				}
				err = tmpl.Execute(&sekret, ingress)
				if err != nil {
					panic(err)
				}

				newtls := v1beta1.IngressTLS{
					Hosts:      diff,
					SecretName: sekret.String(),
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
	logger.Infof("Listening on :4443")
	err = http.ListenAndServeTLS(":4443", "/data/tls.crt", "/data/tls.key", whHandler)
	if err != nil {
		panic(err)
	}
}
