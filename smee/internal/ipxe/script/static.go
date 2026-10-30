package script

// StaticScript is the iPXE script used when in the auto-proxy mode.
// It is built to be generic enough for all hardware to use.
// Like [HookScript], it writes the Tink Agent values as tinkerbell_<flag>
// parameters.
var StaticScript = `#!ipxe
` + syslogScript + `echo Loading the static Tinkerbell iPXE script...

set arch ${buildarch}
# Tinkerbell only supports 64 bit architectures.
# The build architecture does not necessarily represent the architecture of the machine on which iPXE is running.
# https://ipxe.org/cfg/buildarch
iseq ${arch} i386 && set arch x86_64 ||
iseq ${arch} arm32 && set arch aarch64 ||
iseq ${arch} arm64 && set arch aarch64 ||

set kernel {{ if .KernelName }}{{ .KernelName }}-${arch}{{ else }}vmlinuz-${arch}{{ end }}
set initrd {{ if .InitrdName }}{{ .InitrdName }}-${arch}{{ else }}initramfs-${arch}{{ end }}
set download-url {{ .DownloadURL }}
set retries:int32 {{ .Retries }}
set retry_delay:int32 {{ .RetryDelay }}

set tinkerbell_id ${mac}
set tinkerbell_grpc_server {{ .TinkGRPCAuthority }}
set syslog_host {{ .SelectedSyslogHost }}
set tinkerbell_grpc_tls {{ .TinkerbellTLS }}

echo tinkerbell_id=${mac}
echo tinkerbell_grpc_server={{ .TinkGRPCAuthority }}
echo syslog_host={{ .SelectedSyslogHost }}
echo tinkerbell_grpc_tls={{ .TinkerbellTLS }}

set idx:int32 0
:retry_kernel
kernel ${download-url}/${kernel} \
syslog_host=${syslog_host} tinkerbell_grpc_server=${tinkerbell_grpc_server} tinkerbell_grpc_tls=${tinkerbell_grpc_tls} tinkerbell_id=${tinkerbell_id} \
console=tty1 console=tty2 console=ttyAMA0,115200 console=ttyAMA1,115200 console=ttyS0,115200 console=ttyS1,115200 \
intel_iommu=on iommu=pt {{- range .ExtraKernelParams}} {{.}} {{- end}} initrd=${initrd} && goto download_initrd || iseq ${idx} ${retries} && goto kernel-error || inc idx && echo retry in ${retry_delay} seconds ; sleep ${retry_delay} ; goto retry_kernel

:download_initrd
set idx:int32 0
:retry_initrd
initrd ${download-url}/${initrd} && goto boot || iseq ${idx} ${retries} && goto initrd-error || inc idx && echo retry in ${retry_delay} seconds ; sleep ${retry_delay} ; goto retry_initrd

:boot
set idx:int32 0
:retry_boot
boot || iseq ${idx} ${retries} && goto boot-error || inc idx && echo retry in ${retry_delay} seconds ; sleep ${retry_delay} ; goto retry_boot

:kernel-error
echo Failed to load kernel
imgfree
exit

:initrd-error
echo Failed to load initrd
imgfree
exit

:boot-error
echo Failed to boot
imgfree
exit
`
