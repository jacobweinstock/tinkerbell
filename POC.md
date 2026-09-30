CaptainOS Bootstrap Data Delivery Through a Patchable Initramfs

Status: proposed design. Format support and current upstream behavior have been
reviewed; this design has not yet been implemented or boot-tested.

## Goal

Deliver structured, per-host data to CaptainOS before its networking starts,
without rebuilding an ISO whenever that data changes.

Examples include network configuration with multiple interfaces, VLANs, bonds,
and BGP settings. The delivery mechanism should support text or binary data and
avoid kernel command line size and quoting constraints.

The model should follow Tinkerbell's existing ISO personalization:

> Build the shared CaptainOS ISO once, then substitute host-specific bytes while
> Tinkerbell serves it.

This proposal focuses on delivery. Configuration schemas, BGP implementation,
and secret handling are separate concerns.

## Current behavior

CaptainOS builds a compressed CPIO initramfs and includes it in a bootable ISO.
GRUB loads the kernel and that initramfs. CaptainOS's early init copies the
extracted filesystem into tmpfs before starting systemd.

Smee currently personalizes the ISO by replacing a reserved magic string with
kernel command line parameters while proxying the source ISO. This avoids
per-host ISO builds.

BMC virtual media commonly reads the ISO through many HTTP range requests rather
than a single full download. Any larger patching mechanism must work when a
request starts or ends inside the patchable region.

## Proposed ISO layout

Make a one-time change to CaptainOS's normal ISO build:

```text
/boot/vmlinuz
/boot/initramfs          Existing compressed OS initramfs
/boot/bootstrap.cpio    Fixed-size, uncompressed configuration slot
```

Configure GRUB to load both initramfs files:

```grub
initrd /boot/initramfs /boot/bootstrap.cpio
```

At build time, `bootstrap.cpio` contains an empty, valid `newc` CPIO archive
followed by zero padding to a reserved capacity. A capacity such as 4 MiB is an
initial example, not a decided requirement.

Linux supports a sequence of compressed and uncompressed CPIO archives, with
zero padding between or after archives. Consequently, the OS initramfs can stay
compressed while the configuration slot remains directly patchable.

Changing configuration does not change:

- The total ISO size.
- ISO filesystem directory entries or file extents.
- GRUB configuration.
- The kernel or base OS initramfs.

Tinkerbell does not invoke ISO authoring tools or require a per-host ISO file.

## Recommended slot representation

Keep the outer ISO file's size fixed, but allow the CPIO archive inside it to vary.

```text
bootstrap.cpio: reserved capacity S

+---------------------------------------+
| Host-specific newc CPIO archive        |
|   Directory entries                   |
|   Configuration files                 |
|   TRAILER!!!                          |
+---------------------------------------+
| Zero padding to capacity S            |
+---------------------------------------+
```

For each configuration generation, Tinkerbell serializes a small, uncompressed
CPIO archive containing the configuration files. The remainder of the reserved
slot is exposed as zeros.

This is small-archive serialization, not an ISO rebuild. The generated archive
may be cached per configuration generation; the padded slot need not be fully
allocated in memory.

Use the `newc` format (`070701`), preserve required alignment, and include an
explicit `TRAILER!!!` entry. Unlike the CPIO `crc` variant, this format does not
require a data checksum.

Benefits:

- Individual files have their actual lengths.
- Multiple files can be delivered.
- Text and binary data use the same mechanism.
- CaptainOS does not need a special padding convention for extracted files.
- CPIO headers can change without changing the outer ISO filesystem metadata.

The archive producer must account for directory entries, filenames, headers,
alignment, and the trailer when checking capacity, not just payload sizes.

### Alternative: a fixed-size file inside a fixed CPIO archive

An alternative is to reserve one fixed-size file inside the CPIO and patch only
its data. For JSON, the unused space could be whitespace. Arbitrary binary data
would need a length field or another framing convention.

This closely resembles the existing fixed-region replacement, but gives the
extracted file a fixed size and introduces payload-format-specific handling.
Replacing the complete inner archive is preferred because it avoids those
constraints without requiring an ISO rebuild.

## Streaming overlay in Smee

Preserve the existing serving model, but replace marker searching in individual
response buffers with an offset-aware overlay.

For each source ISO, Smee needs:

```text
O = absolute byte offset of the slot within the ISO
S = slot capacity
C = generated configuration archive
L = length of C, where L <= S
```

The effective ISO representation is:

```text
Outside [O, O + S):       Original source ISO bytes
Inside  [O, O + L):       Configuration archive bytes
Inside  [O + L, O + S):   Zero padding
```

For each response buffer, calculate its absolute ISO offset and replace only its
intersection with the slot. A partial response's starting offset comes from the
actual upstream response, not an assumption that the upstream honored the
requested range.

For example:

```text
Configuration slot:       [---------------------------]
BMC range request:                    [------]
```

This request contains no opening marker but must still receive the appropriate
configuration bytes.

The implementation must handle:

- Full responses.
- Requests entirely outside the slot.
- Requests entirely inside the slot.
- Requests crossing either or both slot boundaries.
- Read-buffer boundaries within the archive or its padding.
- Upstream responses that return the full image despite a range request.

Memory use should scale with the generated configuration and stream buffers,
not the ISO size. For small requests, an implementation can map the required
slice of the generated archive without constructing the complete padded slot.

### Why a larger magic string is insufficient

The current patcher searches individual read buffers for a marker. Enlarging
that marker does not solve requests beginning in the middle of the reserved
space, or markers split across read buffers.

A marker may assist discovery or validation of the source artifact, but cannot
be the sole trigger for patching each HTTP response.

## Discovering the reserved extent

Two approaches are viable.

### Option 1: discover from the ISO filesystem

Locate the known bootstrap filename, read its extent and size, and cache the
result for that exact immutable source ISO.

The implementation must validate that the file layout is supported. Requiring
a single contiguous extent is a reasonable initial constraint.

This avoids publishing a separate layout artifact but requires ISO filesystem
inspection in Tinkerbell.

### Option 2: publish layout metadata with CaptainOS artifacts

CaptainOS's release process publishes the slot offset and capacity, bound to
the exact source ISO through a digest or equivalent immutable identity.

This simplifies serving but adds an artifact-distribution contract.

Neither approach requires per-request ISO construction. Offsets must not be
assumed constant across releases, architectures, flavors, or custom source ISOs.
An ISO without a supported slot must be reported as unsupported for this
delivery mode rather than patched at a guessed location.

## HTTP and configuration consistency

### Stable configuration across a mount

A BMC makes many independent range requests. If each request uses the latest
configuration, one boot could assemble parts of different CPIO archives.

Pin both the source ISO and configuration generation for a mount. One approach
is a versioned URL identifying an immutable configuration snapshot. This does
not require storing a personalized ISO; all responses can use the same shared
source ISO and a small generated archive.

Configuration updates take effect through a new mount or generation, rather
than changing bytes beneath an existing mounted image.

### Response metadata

Lengths and range offsets remain unchanged because the replacement has the
same reserved capacity.

Other representation metadata must describe the personalized image:

- Do not reuse an upstream ETag as though every personalized ISO were identical.
- Handle conditional requests and `If-Range` against the personalized
  representation.
- Prevent caches from mixing hosts or configuration generations.
- Make `HEAD` metadata consistent with `GET`.
- Disable HTTP content transformations that invalidate ISO byte offsets.
- Handle multiple-range requests correctly, or deliberately fall back to a
  standards-compliant full response rather than treating multipart data as a
  contiguous ISO range.

### Capacity and error handling

Generate and validate the archive before committing response headers or serving
image data. If it exceeds capacity, return an explicit error; never truncate.

Increasing capacity requires a new shared CaptainOS ISO release. Changes within
capacity require no ISO rebuild.

## CaptainOS consumption

The kernel extracts the additional archive before CaptainOS starts userspace.
Configuration files should use a dedicated path that survives CaptainOS's
rootfs-to-tmpfs transition and is not subsequently hidden by another mount.

A bootstrap reader can consume those files before networkd starts. Alternatively,
selected native configuration files could be delivered directly, provided
their placement and startup ordering are explicitly supported.

The delivery mechanism does not itself interpret network configuration or
implement BGP. Those remain consumers of the delivered files.

An unpersonalized ISO contains an empty archive and should retain existing
boot behavior. Legacy kernel command line personalization can coexist with
the new slot.

## Reuse for iPXE boot

Use the same CPIO serializer for both delivery paths:

```text
ISO:
  Generate configuration CPIO
  -> overlay it into the reserved ISO slot
  -> GRUB loads the second initramfs

iPXE:
  Generate configuration CPIO
  -> serve it directly as an additional initrd
  -> iPXE supplies it alongside the OS initramfs
```

The iPXE response does not need the ISO slot's fixed-capacity padding. Both
paths deliver the same extracted files and use the same CaptainOS consumer.

This provides data before CaptainOS networking, not necessarily before all
networking: iPXE still needs connectivity to download its boot artifacts.
BMC-delivered media instead uses the BMC's management connectivity.

## Trade-offs

- GRUB reads the full reserved file, including padding. Larger slots cost
  transfer time and boot memory even when configuration is small.
- The fixed capacity must cover expected data and CPIO overhead.
- New base ISO releases are needed to increase capacity or change the slot
  contract, but not to update ordinary host configuration.
- Offset discovery and range-aware patching are more involved than replacing
  a short marker, but remain much lighter than on-demand ISO construction.
- Versioned configuration snapshots need a retention policy that accommodates
  BMC retries and the lifetime of mounted media.

## Validation plan

### Archive and overlay tests

- Extract generated archives and verify file names, contents, and lengths.
- Verify empty, text, binary, and multiple-file configurations.
- Test archives exactly at capacity and larger than capacity.
- Confirm that bytes outside the slot never change and ISO size stays constant.
- Compare a full personalized response with the same image reconstructed from
  arbitrary partial responses, including out-of-order requests.
- Exercise boundaries at the slot start, archive end, slot end, and stream
  buffer boundaries.
- Verify that changing configuration does not alter an already pinned
  generation.

### HTTP tests

- Cover full, single-range, suffix-range, open-ended-range, and multiple-range
  behavior.
- Cover upstream range requests answered with a full response.
- Verify `HEAD`, validators, conditional requests, and cache separation.
- Confirm that missing slots, mismatched source identity, and oversized
  archives fail before image bytes are served.

### Boot tests

- Boot an unpersonalized ISO and confirm existing behavior.
- Boot personalized ISOs on supported architectures and firmware combinations.
- Verify that delivered files survive the root transition and are available
  before networking starts.
- Exercise real BMC virtual media as well as virtual-machine boot.
- Deliver equivalent configuration through iPXE and compare extracted files.
- Measure boot time and memory with different slot capacities and payload
  sizes, including payloads well beyond kernel command line limits.

## Open decisions

- Reserved capacity and whether it differs by flavor.
- ISO filesystem discovery versus published layout metadata.
- Configuration generation identification and snapshot retention.
- Initial file layout and whether delivery starts with one document or
  multiple files.
- Supported behavior for custom ISOs without a reserved slot.

## References

Source observations below are pinned to the revisions reviewed for this proposal.

- [Linux initramfs buffer format](https://docs.kernel.org/driver-api/early-userspace/buffer-format.html):
  mixed compressed/uncompressed CPIO archives, padding, headers, and trailers.
- [GRUB initrd command](https://www.gnu.org/software/grub/manual/grub/html_node/initrd.html):
  loading multiple initrds in order.
- [iPXE image and initrd loading](https://ipxe.org/cmd/imgfetch):
  additional archives and file injection.
- [CaptainOS ISO builder](https://github.com/tinkerbell/captain/blob/dd8b3283d4295e1d3e2167de43eba6584c282488/captain/iso.py).
- [CaptainOS initramfs build configuration](https://github.com/tinkerbell/captain/blob/dd8b3283d4295e1d3e2167de43eba6584c282488/captain/flavors/common_debian/mkosi.conf.j2).
- [CaptainOS early init](https://github.com/tinkerbell/captain/blob/dd8b3283d4295e1d3e2167de43eba6584c282488/captain/flavors/common_debian/mkosi.extra/init).
- [Smee ISO patching implementation](https://github.com/tinkerbell/tinkerbell/blob/66a7fc46b6f245f0d500f4508ba17a34db9733a3/smee/internal/iso/iso.go).
- [Tinkerbell virtual-media boot flow](https://github.com/tinkerbell/tinkerbell/blob/66a7fc46b6f245f0d500f4508ba17a34db9733a3/docs/technical/L3_ISO_SUPPORT.md).