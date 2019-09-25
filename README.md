
# TLS Host Controller

This is an admission webhook mutator.  This routine receives a manifest submission in front of the API. If it meets filter criteria, will mutate the manifest, and passes the mutated manifest along.

This specific webhook looks only for `Ingress` manifests.  Its logic is as follows:

```
if spec.rules.hosts exists:
  collect every value in spec.rules[*].hosts into a rulesHosts list
  collect every value in spec.tls[*].host[*] into a tlsHosts list (if exists)

  let diff = set difference rulesHosts - tlsHosts

  create a new `IngressTLS` struct
  add all of the hosts from diff to the struct

  append the struct to the end of the `spec.tls` array

```

This operation should be idempotent.  If there are `rules.hosts` that are not present `tls.hosts`, then they will be added.
If run again on the same ingress, there should be no `rules.hosts` that are not present in `tls.hosts`. So there should be no change.

Note that `rules.host` will never be modified under any circumstance, even if there are rules in `tls.host` that are not present in `rules.host`
Note that any existing `tls` blocks or or `tls.host` entries will not be modified.  The only change is an addition `tls` entry in the array.

# Deploying

This container is intended to be deployed into customer infrastructure (namely on-prem, but should work anywhere)
Therefore, this container is hosted as a public resource in hub.docker.com/agilestacks/tls-host-controller

# Building and Pushing

This is meant to be built and deployed in a container. To create a new docker image, run `make build`

Simply run

```
docker login #(into agilestacks account)
make build push
```
