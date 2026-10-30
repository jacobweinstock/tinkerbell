package script

// HookScript is the default iPXE script for loading Hook.
//
// The Tink Agent values are tinkerbell_<flag> parameters, which CaptainOS
// passes to the Agent as the named flag.
var HookScript = `#!ipxe
` + syslogScript + `
echo Loading the Tinkerbell Hook iPXE script...
{{- if .TraceID }}
echo Debug TraceID: {{ .TraceID }}
{{- end }}

set arch {{ .Arch }}
set download-url {{ .DownloadURL }}
set kernel {{ if .KernelName }}{{ .KernelName }}{{ else }}vmlinuz-${arch}{{ end }}
set initrd {{ if .InitrdName }}{{ .InitrdName }}{{ else }}initramfs-${arch}{{ end }}
set retries:int32 {{ .Retries }}
set retry_delay:int32 {{ .RetryDelay }}

set idx:int32 0
:retry_kernel
kernel ${download-url}/${kernel} \
facility={{ .Facility }} syslog_host={{ .SelectedSyslogHost }} tinkerbell_grpc_server={{ .TinkGRPCAuthority }} tinkerbell_grpc_tls={{ .TinkerbellTLS }} tinkerbell_grpc_insecure_tls={{ .TinkerbellInsecureTLS }} tinkerbell_id={{ .WorkerID }} \
modules=loop,squashfs,sd-mod,usb-storage intel_iommu=on iommu=pt initrd=${initrd} console=tty0 console=ttyS1,115200 {{- range .ExtraKernelParams}} {{.}} {{- end}} && goto download_initrd || iseq ${idx} ${retries} && goto kernel-error || inc idx && echo retry in ${retry_delay} seconds ; sleep ${retry_delay} ; goto retry_kernel

:download_initrd
set idx:int32 0
:retry_initrd
initrd ${download-url}/${initrd} && goto {{ if .BootstrapURL }}download_bootstrap{{ else }}boot{{ end }} || iseq ${idx} ${retries} && goto initrd-error || inc idx && echo retry in ${retry_delay} seconds ; sleep ${retry_delay} ; goto retry_initrd
{{- if .BootstrapURL }}

:download_bootstrap
# The bootstrap initrd is a relative path only: iPXE fetches it from the server that served this script.
set idx:int32 0
:retry_bootstrap
initrd {{ .BootstrapURL }} && goto boot || iseq ${idx} ${retries} && goto bootstrap-error || inc idx && echo retry in ${retry_delay} seconds ; sleep ${retry_delay} ; goto retry_bootstrap
{{- end }}

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
{{- if .BootstrapURL }}

:bootstrap-error
echo Failed to load bootstrap data
imgfree
exit
{{- end }}

:boot-error
echo Failed to boot
imgfree
exit
`

// Hook holds the values used to generate the iPXE script that loads the Hook OS.
type Hook struct {
	AddressFamily         addressFamily // selected from the requested boot script
	Arch                  string        // example x86_64
	Console               string        // example ttyS1,115200
	DownloadURL           string        // example https://location:8080/to/kernel/and/initrd
	ExtraKernelParams     []string      // example tink_worker_image=quay.io/tinkerbell/tink-worker:v0.8.0
	Facility              string
	SyslogHost            string
	SyslogHostV6          string // used only by the DHCPv6 boot script
	TinkerbellTLS         bool
	TinkerbellInsecureTLS bool
	TinkGRPCAuthority     string // example 192.168.2.111:42113
	TraceID               string
	WorkerID              string // example 3c:ec:ef:4c:4f:54 or worker1
	Retries               int    // number of retries to attempt when fetching kernel and initrd files
	RetryDelay            int    // number of seconds to wait between retries
	KernelName            string // name of the kernel file
	InitrdName            string // name of the initrd file
	// BootstrapURL is the per-host configuration archive, loaded as an
	// additional initrd. It must come after the OS initramfs: the kernel
	// applies archives in order, so an earlier one would be overwritten.
	// Empty omits the whole stanza.
	BootstrapURL string
}
