// Minimal client for the Microsoft Defender scan engine (mpengine.dll, x64).
//
// Loads engine\mpengine.dll from the current directory, boots it against the
// definition files in engine\ and scans each file given on the command line
// through the stream-buffer interface. Built with mingw-w64 and run under
// wine64 by scripts/defender-scan.sh; it is plain Win32 and also runs on
// Windows.
//
// The __rsignal interface and the structure layouts come from Tavis Ormandy's
// loadlibrary project (https://github.com/taviso/loadlibrary, GPL-2.0) and the
// x64 layouts from the x64_waffle fork by v-p-b. Only the boot and
// stream-scan calls are used here.
//
// Exit codes: 0 clean, 10 at least one threat reported, 1 unreadable input,
// 2 engine could not be loaded, 3 engine refused to boot.
#include <windows.h>
#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>

#define RSIG_BOOTENGINE 0x4036
#define RSIG_SCAN_STREAMBUFFER 0x403D
#define BOOTENGINE_PARAMS_VERSION 0x8E00
#define BOOT_ATTR_NORMAL (1 << 0)
#define ENGINE_UNPACK (1 << 1)

#pragma pack(push, 1)
typedef struct {
    DWORD f0, f4, f8, fC;
} ENGINE_INFO;
typedef struct {
    uint64_t EngineFlags;
    PWCHAR Inclusions;
    PVOID Exceptions;
    PWCHAR UnknownString2;
    PWCHAR QuarantineLocation;
    DWORD field_14, field_18, field_1C, field_20, field_24, field_28;
    uint64_t field_2C, field_30, field_34;
    PCHAR UnknownAnsiString1, UnknownAnsiString2;
} ENGINE_CONFIG;
typedef struct {
    uint64_t ClientVersion;
    PWCHAR SignatureLocation;
    PVOID SpynetSource;
    ENGINE_CONFIG *EngineConfig;
    ENGINE_INFO *EngineInfo;
    PWCHAR ScanReportLocation;
    DWORD BootFlags;
    PWCHAR LocalCopyDirectory;
    PWCHAR OfflineTargetOS;
    CHAR ProductString[16];
    uint64_t field_34;
    PVOID GlobalCallback;
    PVOID EngineContext;
    uint64_t AvgCpuLoadFactor;
    CHAR field_44[16];
    PWCHAR SpynetReportingGUID, SpynetVersion, NISEngineVersion, NISSignatureVersion;
    uint64_t FlightingEnabled;
    DWORD FlightingLevel;
    PVOID DynamicConfig;
    DWORD AutoSampleSubmission, EnableThreatLogging;
    PWCHAR ProductName;
    DWORD PassiveMode, SenseEnabled;
    PWCHAR SenseOrgId;
    DWORD Attributes, BlockAtFirstSeen, PUAProtection, SideBySidePassiveMode;
} BOOTENGINE_PARAMS;
typedef struct {
    DWORD field_0;
    DWORD Flags;
    PCHAR FileName;
    CHAR VirusName[28];
    DWORD field_2C, field_30, field_34, field_38, field_3C, field_40, field_44, field_48, field_4C;
    uint64_t FileSize;
    uint64_t UserPtr;
    DWORD field_60, field_64;
    PCHAR MaybeFileName2;
    PWCHAR StreamName1, StreamName2;
    DWORD field_6C;
    DWORD ThreatId;
} SCANSTRUCT;
typedef struct {
    DWORD (*EngineScanCallback)(SCANSTRUCT *);
    DWORD field_4;
    uint64_t UserPtr;
    DWORD field_C;
} SCAN_REPLY;
typedef struct {
    FILE *UserPtr;
    DWORD (*Read)(FILE *, uint64_t, PVOID, DWORD, PDWORD);
    DWORD (*Write)(FILE *, uint64_t, PVOID, DWORD, PDWORD);
    DWORD (*GetSize)(FILE *, uint64_t *);
    DWORD (*SetSize)(FILE *, uint64_t *);
    PWCHAR (*GetName)(FILE *);
    DWORD (*SetAttributes)(FILE *, DWORD, PVOID, DWORD);
    DWORD (*GetAttributes)(FILE *, DWORD, PVOID, DWORD, PDWORD);
} STREAMBUFFER_DESCRIPTOR;
typedef struct {
    STREAMBUFFER_DESCRIPTOR *Descriptor;
    SCAN_REPLY *ScanReply;
    uint64_t UnknownB, UnknownC;
} SCANSTREAM_PARAMS;
#pragma pack(pop)

enum {
    SCAN_ENCRYPTED = 1 << 6,
    SCAN_MEMBERNAME = 1 << 7,
    SCAN_FILENAME = 1 << 8,
    SCAN_FILETYPE = 1 << 9,
    SCAN_CORRUPT = 1 << 13,
    SCAN_PACKERSTART = 1 << 19,
};
#define SCAN_THREAT_MASK 0x08000022
#define SCAN_PUA_MASK 0x40010000

static int threats;
static int verbose;

static DWORD EngineScanCallback(SCANSTRUCT *Scan)
{
    if (verbose) {
        if (Scan->Flags & SCAN_MEMBERNAME) printf("  archive member %s\n", Scan->VirusName);
        if (Scan->Flags & SCAN_FILENAME) printf("  scanning %s\n", Scan->FileName);
        if (Scan->Flags & SCAN_PACKERSTART) printf("  packer %s\n", Scan->VirusName);
        if (Scan->Flags & SCAN_ENCRYPTED) printf("  encrypted\n");
        if (Scan->Flags & SCAN_CORRUPT) printf("  may be corrupt\n");
        if (Scan->Flags & SCAN_FILETYPE) printf("  %s identified as %s\n", Scan->FileName, Scan->VirusName);
    }
    if (Scan->Flags & SCAN_THREAT_MASK) {
        printf("  THREAT %s\n", Scan->VirusName);
        threats++;
    }
    if ((Scan->Flags & SCAN_PUA_MASK) == SCAN_PUA_MASK) {
        printf("  THREAT (PUA) %s\n", Scan->VirusName);
        threats++;
    }
    fflush(stdout);
    return 0;
}

static DWORD ReadStream(FILE *fp, uint64_t Offset, PVOID Buffer, DWORD Size, PDWORD SizeRead)
{
    _fseeki64(fp, Offset, SEEK_SET);
    *SizeRead = (DWORD) fread(Buffer, 1, Size, fp);
    return TRUE;
}

static DWORD GetStreamSize(FILE *fp, uint64_t *FileSize)
{
    _fseeki64(fp, 0, SEEK_END);
    *FileSize = _ftelli64(fp);
    return TRUE;
}

static PWCHAR GetStreamName(FILE *fp)
{
    (void) fp;
    return L"input";
}

int main(int argc, char **argv)
{
    int first = 1;
    if (argc > 1 && strcmp(argv[1], "-v") == 0) {
        verbose = 1;
        first = 2;
    }
    if (argc <= first) {
        fprintf(stderr, "usage: mpclient-win.exe [-v] <file>...\n");
        return 1;
    }

    HMODULE engine = LoadLibraryA("engine\\mpengine.dll");
    if (!engine) {
        fprintf(stderr, "LoadLibrary(engine\\mpengine.dll) failed: %lu\n", GetLastError());
        return 2;
    }
    DWORD (*rsignal)(PHANDLE, DWORD, PVOID, DWORD) = (void *) GetProcAddress(engine, "__rsignal");
    if (!rsignal) {
        fprintf(stderr, "__rsignal export not found\n");
        return 2;
    }

    ENGINE_INFO info = {0};
    ENGINE_CONFIG cfg = {0};
    BOOTENGINE_PARAMS boot = {0};
    boot.ClientVersion = BOOTENGINE_PARAMS_VERSION;
    boot.Attributes = BOOT_ATTR_NORMAL;
    boot.SignatureLocation = L"engine";
    boot.ProductName = L"defender-scan";
    boot.EngineInfo = &info;
    boot.EngineConfig = &cfg;
    cfg.QuarantineLocation = L"quarantine";
    cfg.Inclusions = L"*.*";
    // Unpack archives so the members are scanned as well as the container.
    cfg.EngineFlags = getenv("MP_ENGINEFLAGS") ? strtoull(getenv("MP_ENGINEFLAGS"), NULL, 0) : ENGINE_UNPACK;

    HANDLE kernel = NULL;
    DWORD rc = rsignal(&kernel, RSIG_BOOTENGINE, &boot, sizeof boot);
    if (rc != 0) {
        fprintf(stderr, "engine boot failed (%lu); are mpengine.dll and the .vdm files in engine\\?\n", rc);
        return 3;
    }

    SCAN_REPLY reply = {0};
    STREAMBUFFER_DESCRIPTOR desc = {0};
    SCANSTREAM_PARAMS params = {0};
    params.Descriptor = &desc;
    params.ScanReply = &reply;
    reply.EngineScanCallback = EngineScanCallback;
    reply.field_C = 0x7fffffff;
    desc.Read = ReadStream;
    desc.GetSize = GetStreamSize;
    desc.GetName = GetStreamName;

    int unreadable = 0;
    for (int i = first; i < argc; i++) {
        int before = threats;
        desc.UserPtr = fopen(argv[i], "rb");
        if (!desc.UserPtr) {
            printf("%s: cannot open\n", argv[i]);
            unreadable = 1;
            continue;
        }
        printf("%s\n", argv[i]);
        fflush(stdout);
        rc = rsignal(&kernel, RSIG_SCAN_STREAMBUFFER, &params, sizeof params);
        fclose(desc.UserPtr);
        if (rc != 0) {
            printf("  scan call failed (%lu)\n", rc);
            unreadable = 1;
        } else if (threats == before) {
            printf("  clean\n");
        }
        fflush(stdout);
    }
    printf("threats: %d\n", threats);
    if (threats) return 10;
    return unreadable ? 1 : 0;
}
