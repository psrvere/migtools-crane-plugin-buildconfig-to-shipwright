"""Write this plugin's entry into a checkout of the crane plugin index.

The release workflow runs this against a checkout of migtools/crane-plugins
after it has built the binaries, and opens a pull request with the result.

Everything the entry says about the plugin comes from the plugin's own
metadata response, so the flag names, help and examples cannot drift from what
the binary actually reports. Only the version, the repository and the asset
naming are supplied here, and those are the workflow's to know.

Environment:
  VERSION     the release being published, e.g. v0.1.0
  REPO        owner/name of the plugin repository, for the download URLs
  META        a file holding the plugin's metadata response
  INDEX_ROOT  a checkout of the plugin index
"""

import json, os, sys, yaml

version = os.environ["VERSION"]
repo = os.environ["REPO"]
meta = json.load(open(os.environ["META"]))
root = os.environ["INDEX_ROOT"]

name = meta["name"]                      # BuildConfigToBuildsPlugin
folder = name[:-6] if name.endswith("Plugin") else name   # BuildConfigToBuilds
manifest_path = os.path.join(root, "plugins", folder, "index.yaml")

platforms = [("linux","amd64"),("linux","arm64"),("darwin","amd64"),("darwin","arm64"),("windows","amd64")]
binaries = []
for os_, arch in platforms:
    asset = f"{arch}-{os_}-{name.lower()}-{version}"
    if os_ == "windows":
        asset += ".exe"
    binaries.append({"arch": arch, "os": os_,
                     "uri": f"https://github.com/{repo}/releases/download/{version}/{asset}"})

entry = {
    "binaries": binaries,
    "description": meta.get("description") or
        ("This plugin converts OpenShift BuildConfig resources to Shipwright Build "
         "resources. It runs offline and never contacts a cluster."),
    "name": name,
    "optionalFields": [{"example": f["example"], "flagName": f["flagName"], "help": f["help"]}
                       for f in meta.get("optionalFields", [])],
    "shortDescription": name,
    "version": version,
}

if os.path.exists(manifest_path):
    doc = yaml.safe_load(open(manifest_path))
    doc["versions"] = [v for v in doc.get("versions", []) if v.get("version") != version]
    doc["versions"].append(entry)
else:
    os.makedirs(os.path.dirname(manifest_path), exist_ok=True)
    doc = {"apiServer": "crane.konveyor.io/v1alpha1", "kind": "Plugin", "versions": [entry]}
yaml.safe_dump(doc, open(manifest_path, "w"), default_flow_style=False, sort_keys=True, width=80)

index_path = os.path.join(root, "index.yaml")
idx = yaml.safe_load(open(index_path))
idx.setdefault("plugins", [])
if not any(p.get("name") == name for p in idx["plugins"]):
    idx["plugins"].append({"name": name,
        "path": f"https://raw.githubusercontent.com/migtools/crane-plugins/main/plugins/{folder}/index.yaml"})
    yaml.safe_dump(idx, open(index_path, "w"), default_flow_style=False, sort_keys=False, width=100)
    print(f"added {name} to the top-level index")
else:
    print(f"{name} already listed in the top-level index")
print(f"wrote {manifest_path} with {len(doc['versions'])} version(s)")
