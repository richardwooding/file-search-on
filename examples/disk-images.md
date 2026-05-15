# Recipes — Disk images

Disk-image content types: `disk-image/dmg` (Apple UDIF — `.dmg`), `disk-image/iso9660` (CD/DVD masters — `.iso`), `disk-image/vhd` (Microsoft Connectix/Hyper-V legacy — `.vhd`), `disk-image/vhdx` (Microsoft Hyper-V v2 — `.vhdx`), `disk-image/vmdk` (VMware sparse-extent — `.vmdk`), `disk-image/qcow2` (QEMU Copy-On-Write — `.qcow2` / `.qcow`), `disk-image/wim` (Windows Imaging Format — `.wim`). Umbrella boolean `is_disk_image`.

Hand-rolled on top of `encoding/binary` — no CGo, no third-party libs. Each parser reads the format's header (or footer) only; we don't traverse the filesystem inside. Out of scope: OCR (N/A — disk images aren't documents), reading files inside the image (HFS+ / APFS / NTFS / ISO 9660 enumeration), Apple sparseimage / sparsebundle, install packages (`.pkg`, `.deb`, `.rpm`, `.appimage`).

## All-disk-images triage

The umbrella query — every disk image under a directory:

```sh
file-search-on 'is_disk_image' -d ~/Downloads
file-search-on 'is_disk_image' -d ~/VMs
```

By format (CEL string or per-type predicate, same result):

```sh
file-search-on 'is_dmg'    -d ~/Downloads
file-search-on 'is_iso'    -d ~/installers
file-search-on 'is_vhd || is_vhdx' -d ~/VMs/hyperv
file-search-on 'is_vmdk'   -d ~/VMs/vmware
file-search-on 'is_qcow2'  -d ~/VMs/qemu
file-search-on 'is_wim'    -d ~/installers
```

Or by the `disk_image_format` string (handy when grouping):

```sh
file-search-on 'disk_image_format == "udif"' -d ~/Downloads      # DMG
file-search-on 'disk_image_format.startsWith("vhd-")' -d ~/VMs   # VHD fixed/dynamic/differencing
file-search-on 'disk_image_format == "vmdk-sparse"' -d ~/VMs/vmware
```

## Sort + top-K

```sh
# 5 largest VMs on disk (by virtual capacity, not file size)
file-search-on 'is_disk_image' -d ~/VMs --sort-by virtual_size --order desc --limit 5

# 10 most recent installers
file-search-on 'is_disk_image' -d ~/Downloads --sort-by mod_time --order desc --limit 10

# WIM files sorted by image count (biggest install bundles)
file-search-on 'is_wim' -d ~/installers --sort-by image_count --order desc
```

## Find by sub-format

VHD distinguishes fixed (pre-allocated full capacity) from dynamic (grows on demand) from differencing (chained off a parent disk). The `disk_type` attribute carries the kind:

```sh
# Fixed-size VHDs — full capacity already on disk, candidates for compression
file-search-on 'is_vhd && disk_type == "fixed"' -d ~/VMs

# Dynamic VHDs — current file size < virtual_size
file-search-on 'is_vhd && disk_type == "dynamic"' -d ~/VMs

# Differencing VHDs — these depend on a parent, often broken if the parent moves
file-search-on 'is_vhd && disk_type == "differencing"' -d ~/VMs
```

VMDK sparse extents:

```sh
# Compressed-sparse VMDKs (read-only OVA/OVF appliance disks)
file-search-on 'is_vmdk && disk_type == "sparse-compressed"' -d ~/VMs
```

## ISO volume reconnaissance

ISO 9660 carries a volume label and creation date in the primary volume descriptor:

```sh
# Find a Linux installer ISO by its label
file-search-on 'is_iso && volume_label.startsWith("Ubuntu")' -d ~/installers

# ISOs created in 2025 (good for archive cleanup)
file-search-on 'is_iso && created_at >= timestamp("2025-01-01T00:00:00Z") && created_at < timestamp("2026-01-01T00:00:00Z")' -d ~/installers

# ISOs by size — find the bloated 4 GB+ ones
file-search-on 'is_iso && virtual_size > 4000000000' -d ~/installers --sort-by virtual_size --order desc
```

## Find encrypted QCOW2 disks

QCOW2 declares its encryption method in the header (0 = none, 1 = AES, 2 = LUKS). The `is_encrypted` boolean fires when the field is non-zero:

```sh
# Audit: which QCOW2 disks are encrypted?
file-search-on 'is_qcow2 && is_encrypted' -d ~/VMs

# Inverse: unencrypted QCOW2 disks (candidates for encryption)
file-search-on 'is_qcow2 && !is_encrypted' -d ~/VMs
```

## Compression-ratio reconnaissance

Compare on-disk footprint (`size`) to logical capacity (`virtual_size`) to find images with poor compression or sparse-allocation efficiency:

```sh
# QCOW2 disks where the file is > 80% of the claimed virtual size — almost full
file-search-on 'is_qcow2 && virtual_size > 0 && size * 100 / virtual_size > 80' -d ~/VMs

# Sparse VMDKs that have grown to >5 GiB on disk
file-search-on 'is_vmdk && size > 5000000000' -d ~/VMs
```

## Stats — what disk-image families are in this tree?

```sh
# Disk-image format histogram
file-search-on stats 'is_disk_image' -d ~/Downloads --group-by disk_image_format

# VHD sub-type histogram
file-search-on stats 'is_vhd' -d ~/VMs --group-by disk_type

# Total virtual disk capacity per directory (use group-by dir)
file-search-on stats 'is_disk_image' -d ~/VMs --group-by dir
```

## WIM bundles

WIM is a file-level archive that contains 1..N full system images (e.g. Windows install media's `sources/install.wim` carries Home, Pro, Enterprise editions). `image_count` surfaces the bundle size:

```sh
# Multi-edition install WIMs
file-search-on 'is_wim && image_count > 1' -d ~/installers
```

`virtual_size` is 0 for WIM — the format isn't a disk image with a logical sector count. Use the always-on `size` attribute for the on-disk footprint.

## Caveats

- **DMG detection is by extension only.** The UDIF `koly` magic lives at the trailing 512 bytes; the registry's start-of-file sniffer can't see it. A file with random bytes named `foo.dmg` will detect as `disk-image/dmg` but surface `virtual_size: 0` (the trailer parser rejects the bad signature). Same applies to ISO (`CD001` at offset 0x8001) and VHD (`conectix` at EOF).
- **VHDX `virtual_size` is best-effort.** The walker reads the region table at 0x30000 → finds the Metadata region → reads the VirtualDiskSize item. Any link in that chain can fail (truncated file, corrupt region table, region table missing the Metadata GUID) and `virtual_size` falls back to 0. The `disk_image_format = "vhdx"` flag still surfaces.
- **VMDK descriptor-only files detect as text.** VMware uses two on-disk forms: a binary sparse extent (which `disk-image/vmdk` reads) and a plain-text `# Disk DescriptorFile` that references external extents. Only the binary form gets here; descriptor `.vmdk` text files fall through to the existing `text` content type.
- **Encrypted DMG / VHDX password handling is out of scope.** Use the formats' own tooling (`hdiutil`, `Mount-VHD`) to unlock first.
- **Reading files INSIDE the image is out of scope.** This recipe page is for finding and triaging disk-image files themselves. Mounting and walking their filesystems is a different problem.
- **`.img` extension is not registered.** Too generic — covers Android system images, raw disk dumps, embedded firmware, BMP/Truevision Targa graphics, … Magic sniffing would help for some but false positives outweigh value.
