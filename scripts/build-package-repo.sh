#!/usr/bin/env bash
# Build a signed APT and DNF repository from a pool of .deb and .rpm files.
#
# The output is a static site (served by GitHub Pages) with three parts:
#   deb/   an APT repository (suite "stable", component "main", amd64 + arm64)
#   rpm/   a DNF/YUM repository
#   taiga-cli.gpg.key   the ASCII-armored public signing key
#
# A GPG signing key must already be imported into the running gpg keyring; its
# id or email is passed as GPG_KEY_ID. The Release file, the repomd.xml and every
# .rpm are signed with it so apt and dnf verify the chain.
#
# Env:
#   POOL         directory holding the *.deb and *.rpm to publish (required)
#   OUT          output site root, created if absent (required)
#   GPG_KEY_ID   signing key id/email, already in the gpg keyring (required)
set -euo pipefail

POOL="${POOL:?set POOL to the directory of .deb/.rpm files}"
OUT="${OUT:?set OUT to the output site root}"
GPG_KEY_ID="${GPG_KEY_ID:?set GPG_KEY_ID to the signing key}"

ORIGIN="KoukeNeko"
LABEL="Taiga CLI"
SUITE="stable"
COMPONENT="main"
ARCHES="amd64 arm64"

mkdir -p "$OUT"

# ---------------------------------------------------------------- APT ----------
deb_root="$OUT/deb"
rm -rf "$deb_root"
for arch in $ARCHES; do
  mkdir -p "$deb_root/pool/main/$arch" "$deb_root/dists/$SUITE/$COMPONENT/binary-$arch"
done
# The package filenames end in _<arch>.deb, so a glob sorts them into per-arch
# pools; dpkg-scanpackages then writes one Packages index per architecture.
for arch in $ARCHES; do
  for deb in "$POOL"/*_"$arch".deb; do
    [ -e "$deb" ] && cp "$deb" "$deb_root/pool/main/$arch/"
  done
done
(
  cd "$deb_root"
  for arch in $ARCHES; do
    dpkg-scanpackages --multiversion "pool/main/$arch" > "dists/$SUITE/$COMPONENT/binary-$arch/Packages"
    gzip -9kf "dists/$SUITE/$COMPONENT/binary-$arch/Packages"
  done
  apt-ftparchive \
    -o "APT::FTPArchive::Release::Origin=$ORIGIN" \
    -o "APT::FTPArchive::Release::Label=$LABEL" \
    -o "APT::FTPArchive::Release::Suite=$SUITE" \
    -o "APT::FTPArchive::Release::Codename=$SUITE" \
    -o "APT::FTPArchive::Release::Architectures=$ARCHES" \
    -o "APT::FTPArchive::Release::Components=$COMPONENT" \
    release "dists/$SUITE" > "dists/$SUITE/Release"
  gpg --batch --yes --local-user "$GPG_KEY_ID" --clearsign      -o "dists/$SUITE/InRelease"   "dists/$SUITE/Release"
  gpg --batch --yes --local-user "$GPG_KEY_ID" --detach-sign -a -o "dists/$SUITE/Release.gpg" "dists/$SUITE/Release"
)

# ---------------------------------------------------------------- DNF ----------
rpm_root="$OUT/rpm"
rm -rf "$rpm_root"
mkdir -p "$rpm_root"
cp "$POOL"/*.rpm "$rpm_root/"
# Sign each package so `gpgcheck=1` verifies the .rpm itself, then sign the
# repository metadata so `repo_gpgcheck=1` verifies the index. The sign command
# is pinned to the gpg on PATH with loopback pinentry so it runs unattended.
rpm --define "_gpg_name $GPG_KEY_ID" \
    --define "__gpg $(command -v gpg)" \
    --define '__gpg_sign_cmd %{__gpg} --batch --pinentry-mode loopback --no-armor --no-secmem-warning --digest-algo sha256 -u "%{_gpg_name}" -sbo %{__signature_filename} %{__plaintext_filename}' \
    --addsign "$rpm_root"/*.rpm
createrepo_c --quiet "$rpm_root"
gpg --batch --yes --local-user "$GPG_KEY_ID" --detach-sign -a "$rpm_root/repodata/repomd.xml"

# ------------------------------------------------------------ public key -------
gpg --export --armor "$GPG_KEY_ID" > "$OUT/taiga-cli.gpg.key"

echo "Built APT + DNF repositories under $OUT"
