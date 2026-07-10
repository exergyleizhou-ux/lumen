# Third-party assets

## Ketcher standalone (same-origin chemical editor)

Not committed (large ~90MB). Obtain and place as:

```bash
# from repo root
curl -sL -o /tmp/ks.zip https://github.com/epam/ketcher/releases/download/v3.15.0/ketcher-standalone-3.15.0.zip
unzip -q /tmp/ks.zip -d /tmp/ks
rm -rf third_party/ketcher-standalone
mv /tmp/ks/standalone third_party/ketcher-standalone
```

Deploy to VPS:

```bash
rsync -avz -e "ssh -i ~/.ssh/oasis_deploy" third_party/ketcher-standalone/ root@HOST:/var/lib/lumen/ketcher/
# or into sciDir
# root@HOST:/root/.lumen/science/lab/ketcher/
```

Lab serves `/ketcher/` when `index.html` is found under resolveKetcherDir (see server.go).
