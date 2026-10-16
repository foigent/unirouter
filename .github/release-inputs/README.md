# Community byte-candidate inputs

These public inputs belong to the internal, manual `community-byte-candidate.yml`
validation producer. They are not a signed release or an installation entry point.

* `community-public-keys.json` is the already adopted public verification ring.
  It contains no private key. The producer verifies its exact bytes and decoded
  public-key fingerprint before building.
* `component-notices.tar.gz` contains the reviewed notice materials and their
  source/observation bindings for product commit
  `3818ba9627d1bc44eb836036b8ae8904a7175e65`. It preserves original notice bodies.
  Its 318 component entries are a mixture of source inventory and actual build
  observations, explicitly labeled in `index.json`; they are not all claims of
  inclusion in a final image. They are not a complete image SBOM or a declaration
  that every license/source obligation is fulfilled.

The producer records actual final-image inventories and frontend observations
separately. Blank APK license declarations remain blank. The four companion
images remain pinned upstream references. Retaining their reference or a license
text does not silently authorize republishing their image bytes.

The workflow requires a fresh external admission record, bound by SHA256, covering
the exact producer, UR/XI queue and the existing shared **USD 35 monthly hard
limit**. The digest is a reference to that reviewed record, not an automatic
billing check. Dispatch must stop if applicable rates, incremental charges or
shared reservations have not been resolved. This producer creates no Actions
artifact, registry, paid resource or new repository.

All retained binaries remain in a native **draft** release in the same repository.
The helper first checks a harmless marker's anonymous access, then checks every
asset's identity, anonymous denial and downloaded bytes. Draft visibility is a
point-in-time access observation; repository writers can still change the draft.
The producer has no publication, signing, installation or deployment path.
