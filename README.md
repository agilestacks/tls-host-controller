
# TLS Host Controller

This is an admission webhook mutator.  This routine receives a manifest submission in front of the API. If it meets filter criteria, will mutate the manifest, and passes the mutated manifest along.

This specific webhook looks only for `Ingress` manifests.  Its logic is as follows:

```
if spec.rules.hosts exists:
  collect every value in spec.rules.hosts into a rulesHosts list
  collect every value in spec.tls.hosts into a tlsHosts list (if it exists)

  create spec.tls.hosts array and spec.tls.secret with a unique name

  set difference rulesHosts - tlsHosts
  for every host in the diff:
    create a matching entry in spec.tls.hosts
```


