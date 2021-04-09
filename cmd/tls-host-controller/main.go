package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/cloudflare/certinel"
	"github.com/cloudflare/certinel/fswatcher"
	"github.com/sirupsen/logrus"
	kwhhttp "github.com/slok/kubewebhook/v2/pkg/http"
	kwhlogrus "github.com/slok/kubewebhook/v2/pkg/log/logrus"
	kwhmodel "github.com/slok/kubewebhook/v2/pkg/model"
	"github.com/slok/kubewebhook/v2/pkg/webhook/mutating"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	netv1 "k8s.io/api/networking/v1"
	netv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

type nothing struct{}

const cnLimit = 63

// https://cert-manager.io/docs/usage/ingress/
var certManagerAnnotations = []string{"kubernetes.io/tls-acme", "cert-manager.io/issuer", "cert-manager.io/cluster-issuer"}

func main() {
	logrusLogEntry := logrus.NewEntry(logrus.New())
	logrusLogEntry.Logger.SetLevel(logrus.DebugLevel)
	logger := kwhlogrus.NewLogrus(logrusLogEntry)

	var defaultCN string
	flag.StringVar(&defaultCN, "default-cn", "",
		"A comma separated list of CNs that will be considered to create certificate CN shorter than 64 bytes")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr,
			`Usage:
	tls-host-controller [-default-cn app.cluster.account.superhub.io]

	If no -default-cn is set then the shortest host rule well be chomped in front to create CN < 64 bytes long

Flags:
`)
		flag.PrintDefaults()
	}
	flag.Parse()

	cns, err := parseCN(logger, defaultCN)
	if err != nil {
		panic(err)
	}

	mt := mutating.MutatorFunc(func(_ context.Context, _ *kwhmodel.AdmissionReview, obj metav1.Object) (*mutating.MutatorResult, error) {
		ingressv1, v1 := obj.(*netv1.Ingress)
		ingressv1beta1, v1beta1 := obj.(*netv1beta1.Ingress)
		ingressv1beta1ext, v1beta1ext := obj.(*extv1beta1.Ingress)
		if !v1 && !v1beta1 && !v1beta1ext {
			logger.Warningf("unsupported object kind %s: %+v", reflect.TypeOf(obj), obj)
			return &mutating.MutatorResult{}, nil
		}
		logger.Debugf("object kind %s: %+v", reflect.TypeOf(obj), obj)
		var meta *metav1.ObjectMeta
		if v1 {
			meta = &ingressv1.ObjectMeta
		} else if v1beta1 {
			meta = &ingressv1beta1.ObjectMeta
		} else {
			meta = &ingressv1beta1ext.ObjectMeta
		}
		name := meta.Name
		if len(name) == 0 && len(meta.GenerateName) > 0 {
			name = meta.GenerateName
		}

		// cert-manager installed ingress
		logger.Debugf("checking ingress %s", name)
		if strings.HasPrefix(name, "cm-acme-http-solver") {
			logger.Debugf("skipping cert-manager installed ingress %s", name)
			return &mutating.MutatorResult{}, nil
		}

		var specv1 *netv1.IngressSpec
		var specv1beta1 *netv1beta1.IngressSpec
		var specv1beta1ext *extv1beta1.IngressSpec
		if v1 {
			specv1 = &ingressv1.Spec
		} else if v1beta1 {
			specv1beta1 = &ingressv1beta1.Spec
		} else {
			specv1beta1ext = &ingressv1beta1ext.Spec
		}

		// don't interfere with explicit TLS spec
		if (v1 && specv1.TLS != nil) || (v1beta1 && specv1beta1.TLS != nil) || (v1beta1ext && specv1beta1ext.TLS != nil) {
			logger.Debugf("skipping %s as it has TLS block configured already", name)
			return &mutating.MutatorResult{}, nil
		}

		rulesHosts := make(map[string]nothing)
		if v1 {
			for _, r := range specv1.Rules {
				if len(r.Host) > 0 {
					rulesHosts[r.Host] = nothing{}
				}
			}
		} else if v1beta1 {
			for _, r := range specv1beta1.Rules {
				if len(r.Host) > 0 {
					rulesHosts[r.Host] = nothing{}
				}
			}
		} else {
			for _, r := range specv1beta1ext.Rules {
				if len(r.Host) > 0 {
					rulesHosts[r.Host] = nothing{}
				}
			}
		}

		if len(rulesHosts) == 0 {
			logger.Debugf("skipping %s as it has no host rules configured", name)
			return &mutating.MutatorResult{}, nil
		}

		hosts := make([]string, 0, len(rulesHosts))
		for host := range rulesHosts {
			hosts = append(hosts, host)
		}

		// there is a 63 char limit in the CN of cert-manager/LE
		// so we sort the slice of domain names so the shortest is first
		// if it is over 63 characters, we'll need to synthesize a new one and make it first
		sort.Sort(byLength(hosts))
		if len(hosts[0]) > cnLimit {
			cn, err := makeCN(logger, hosts, cns)
			if err != nil {
				logger.Warningf("unable to append %s tls block: %v", name, err)
				return &mutating.MutatorResult{}, nil
			}
			hosts = append([]string{cn}, hosts...)
		}

		// create the IngressTLS Object with our extra hosts and a custom secret
		secret := fmt.Sprintf("auto-%s-tls", name)
		if v1 {
			newtls := netv1.IngressTLS{
				Hosts:      hosts,
				SecretName: secret,
			}
			specv1.TLS = append(specv1.TLS, newtls)
			logger.Debugf("appending %s tls block: %+v", name, newtls)
		} else if v1beta1 {
			newtls := netv1beta1.IngressTLS{
				Hosts:      hosts,
				SecretName: secret,
			}
			specv1beta1.TLS = append(specv1beta1.TLS, newtls)
			logger.Debugf("appending %s tls block: %+v", name, newtls)
		} else {
			newtls := extv1beta1.IngressTLS{
				Hosts:      hosts,
				SecretName: secret,
			}
			specv1beta1ext.TLS = append(specv1beta1ext.TLS, newtls)
			logger.Debugf("appending %s tls block: %+v", name, newtls)
		}

		// append Cert-manager annotation if not present already
		annotations := meta.Annotations
		addAnnotation := true
		if len(annotations) > 0 {
			for _, annotationKey := range certManagerAnnotations {
				if _, exist := annotations[annotationKey]; exist {
					addAnnotation = false
					break
				}
			}
		}
		if addAnnotation {
			if meta.Annotations == nil {
				meta.Annotations = make(map[string]string)
			}
			meta.Annotations[certManagerAnnotations[0]] = "true"
			logger.Debugf("appending %s annotation: %s: true", name, certManagerAnnotations[0])
		}

		if v1 {
			logger.Debugf("returning %s: %+v", reflect.TypeOf(ingressv1), ingressv1)
			return &mutating.MutatorResult{MutatedObject: ingressv1}, nil
		}
		if v1beta1 {
			logger.Debugf("returning %s: %+v", reflect.TypeOf(ingressv1beta1), ingressv1beta1)
			return &mutating.MutatorResult{MutatedObject: ingressv1beta1}, nil
		}
		logger.Debugf("returning %s: %+v", reflect.TypeOf(ingressv1beta1ext), ingressv1beta1ext)
		return &mutating.MutatorResult{MutatedObject: ingressv1beta1ext}, nil
	})

	cfg := mutating.WebhookConfig{
		ID:      "tls-host-controller",
		Mutator: mt,
		Logger:  logger,
	}

	wh, err := mutating.NewWebhook(cfg)
	if err != nil {
		panic(err)
	}

	// Get HTTP handler from webhook.
	whHandler, err := kwhhttp.HandlerFor(kwhhttp.HandlerConfig{Webhook: wh, Logger: logger})
	if err != nil {
		panic(err)
	}

	watcher, err := fswatcher.New("/data/tls.crt", "/data/tls.key")
	if err != nil {
		logger.Errorf("unable to read server certificate: %v", err)
		os.Exit(1)
	}
	sentinel := certinel.New(watcher, func(err error) {
		logger.Warningf("certinel was unable to reload the certificate: %v", err)
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
