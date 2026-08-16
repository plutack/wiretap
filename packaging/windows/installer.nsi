; wiretap NSIS installer for Windows.
;
; Built by the release workflow after the cross-compiled binary is in place:
;
;   makensis -DVERSION=<x.y.z> packaging/windows/installer.nsi
;
; NSIS resolves File/OutFile paths relative to THIS SCRIPT's directory
; (packaging/windows/), not the invoking CWD — hence the ../../ prefixes.
; Expects ../../build/wiretap.exe and writes ../../build/wiretap-<ver>-installer.exe.

!include "LogicLib.nsh"
!include "WinMessages.nsh"
!include "WordFunc.nsh"
!include "FileFunc.nsh"

!ifndef VERSION
  !error "VERSION must be defined: makensis -DVERSION=x.y.z installer.nsi"
!endif

Name "wiretap ${VERSION}"
OutFile "../../build/wiretap-${VERSION}-installer.exe"
InstallDir "$PROGRAMFILES64\wiretap"
InstallDirRegKey HKLM "Software\wiretap" "InstallDir"
RequestExecutionLevel admin
Unicode true

Page directory
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\wiretap"
!define SYS_PATH "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"

Section "install"
  SetOutPath "$INSTDIR"
  File "../../build/wiretap.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Install the WebView2 runtime when missing (Windows 10 without an
  ; Edge-updated runtime cannot render the dashboard). The evergreen
  ; bootstrapper is downloaded by CI from Microsoft's stable permalink and
  ; bundled next to wiretap.exe; same approach the Wails installer uses.
  File "../../build/MicrosoftEdgeWebview2Setup.exe"
  ReadRegStr $4 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${If} $4 == ""
    ReadRegStr $4 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}
  ${If} $4 == ""
    DetailPrint "Installing WebView2 runtime..."
    ExecWait '"$INSTDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install' $5
    ${If} $5 != 0
      DetailPrint "WebView2 bootstrapper exited with $5; continuing"
    ${EndIf}
  ${EndIf}
  Delete "$INSTDIR\MicrosoftEdgeWebview2Setup.exe"

  WriteRegStr HKLM "Software\wiretap" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "wiretap"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "plutack"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKLM "${UNINST_KEY}" "EstimatedSize" "$0"

  ; Idempotently put $INSTDIR on the system PATH: strip any existing entry
  ; first (all three forms), then append — so reinstalling never duplicates.
  ReadRegStr $0 HKLM "${SYS_PATH}" "Path"
  ${WordReplace} "$0" "$INSTDIR;" "" "+" $1
  ${WordReplace} "$1" ";$INSTDIR" "" "+" $2
  ${WordReplace} "$2" "$INSTDIR" "" "+" $3
  ${If} $3 == ""
    StrCpy $3 "$INSTDIR"
  ${Else}
    StrCpy $3 "$3;$INSTDIR"
  ${EndIf}
  WriteRegExpandStr HKLM "${SYS_PATH}" "Path" "$3"
  SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
SectionEnd

Section "un.Uninstall"
  Delete "$INSTDIR\wiretap.exe"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKLM "${UNINST_KEY}"
  DeleteRegKey /ifempty HKLM "Software\wiretap"

  ; Remove $INSTDIR from the system PATH (both ";dir" and "dir;" forms).
  ReadRegStr $0 HKLM "${SYS_PATH}" "Path"
  ${WordReplace} "$0" ";$INSTDIR" "" "+" $1
  ${WordReplace} "$1" "$INSTDIR;" "" "+" $0
  ${WordReplace} "$0" "$INSTDIR" "" "+" $1
  WriteRegExpandStr HKLM "${SYS_PATH}" "Path" "$1"
  SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
SectionEnd
