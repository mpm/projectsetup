#!/usr/bin/env python3
"""Measure shared uncompressed image layer tar bytes, not daemon disk usage."""
import json
import subprocess
import sys
import tarfile

images = sys.argv[1:]
if len(images) != 2:
    sys.exit("usage: measure.py PROJECT_IMAGE_A PROJECT_IMAGE_B")
inspect = json.loads(subprocess.check_output(["docker", "image", "inspect", *images]))
process = subprocess.Popen(["docker", "image", "save", *images], stdout=subprocess.PIPE)
sizes, manifests = {}, []
with tarfile.open(fileobj=process.stdout, mode="r|") as archive:
    for member in archive:
        sizes[member.name] = member.size
        if member.name == "manifest.json":
            manifests = json.load(archive.extractfile(member))
if process.wait():
    sys.exit("docker image save failed")
# Save may deduplicate aliases, so resolve each requested tag explicitly.
layers = [next(m["Layers"] for m in manifests if image in (m["RepoTags"] or [])) for image in images]
shared = set(layers[0]) & set(layers[1])
union = set(layers[0]) | set(layers[1])
prefix = 0
for a, b in zip(inspect[0]["RootFS"]["Layers"], inspect[1]["RootFS"]["Layers"]):
    if a != b:
        break
    prefix += 1
result = {
    "images": [{"tag": tag, "id": data["Id"], "inspectSizeBytes": data["Size"],
                "layers": data["RootFS"]["Layers"],
                "layerTarBytes": sum(sizes[p] for p in paths)}
               for tag, data, paths in zip(images, inspect, layers)],
    "sharedPrefixLayerCount": prefix,
    "sharedLayerCount": len(shared),
    "sharedLayerTarBytes": sum(sizes[p] for p in shared),
    "uniqueLayerTarBytes": sum(sizes[p] for p in union),
    "independentCopiesLayerTarBytes": sum(sum(sizes[p] for p in paths) for paths in layers),
    "scope": "Uncompressed saved image layer tar bytes only; excludes writable containers, volumes, compressed blobs, build cache and filesystem allocation overhead.",
}
print(json.dumps(result, indent=2))
