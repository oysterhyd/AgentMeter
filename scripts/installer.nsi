# -*- coding: utf-8 -*-
Unicode true
RequestExecutionLevel user
SetCompressor /SOLID lzma
SetCompressorDictSize ${APP_COMPRESSION_DICTIONARY}
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!addplugindir /x86-unicode "${PROCESS_PLUGIN_DIR}"

!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_UNINSTALL_ID}"
Name "${APP_NAME}"
OutFile "${INSTALLER_FILE}"
InstallDir "$LOCALAPPDATA\Programs\${APP_NAME}"
InstallDirRegKey HKCU "${UNINSTALL_KEY}" "InstallLocation"
BrandingText "${APP_NAME} · MyGO native UI"
Icon "${BUNDLE_DIR}\icon.ico"
UninstallIcon "${BUNDLE_DIR}\icon.ico"
VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey "ProductName" "${APP_NAME}"
VIAddVersionKey "ProductVersion" "${APP_VERSION}"
VIAddVersionKey "FileVersion" "${APP_VERSION}"
VIAddVersionKey "FileDescription" "${APP_NAME} Setup"
VIAddVersionKey "LegalCopyright" "${APP_NAME}"
ShowInstDetails show
ShowUninstDetails show

!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${APP_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "$(LaunchText)"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

LangString LaunchText ${LANG_SIMPCHINESE} "启动 ${APP_NAME}"
LangString LaunchText ${LANG_ENGLISH} "Launch ${APP_NAME}"
LangString RunningText ${LANG_SIMPCHINESE} "请先从系统托盘完全退出 ${APP_NAME}，再点击重试。安装程序不会强制终止应用。"
LangString RunningText ${LANG_ENGLISH} "Exit ${APP_NAME} from its system tray, then click Retry. Setup will not force the app to stop."
LangString ArchitectureText ${LANG_SIMPCHINESE} "此安装包需要 Windows x64。"
LangString ArchitectureText ${LANG_ENGLISH} "This installer requires Windows x64."
LangString InstallErrorText ${LANG_SIMPCHINESE} "无法写入安装目录，请确认目录权限并完全退出应用。"
LangString InstallErrorText ${LANG_ENGLISH} "Cannot write the installation directory. Check permissions and exit the application."

!macro CheckRunning PREFIX
Function ${PREFIX}CheckRunning
  check_running:
  nsProcess::_FindProcess "${APP_EXE}"
  Pop $0
  ${If} $0 == 603
    nsProcess::_FindProcess "Codex Monitor.exe"
    Pop $0
  ${EndIf}
  nsProcess::_Unload
  ${If} $0 != 603
    IfSilent running_abort
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(RunningText)" IDRETRY check_running
    running_abort:
    SetErrorLevel 2
    Abort
  ${EndIf}
FunctionEnd
!macroend
!insertmacro CheckRunning ""
!insertmacro CheckRunning "un."

Function .onInit
  SetShellVarContext current
  ${IfNot} ${RunningX64}
    MessageBox MB_OK|MB_ICONSTOP "$(ArchitectureText)" /SD IDOK
    SetErrorLevel 1
    Abort
  ${EndIf}
  SetRegView 64
  Call CheckRunning
FunctionEnd

Function un.onInit
  SetShellVarContext current
  SetRegView 64
  Call un.CheckRunning
FunctionEnd

Section "${APP_NAME}"
  Call CheckRunning
  SetOutPath "$INSTDIR"
  SetOverwrite on
  ClearErrors
  File "/oname=${APP_EXE}" "${BUNDLE_DIR}\Codex Monitor Native.exe"
  File "${BUNDLE_DIR}\icon.ico"
  IfErrors install_error
  ; Only remove known legacy program files; never touch the user profile.
  IfFileExists "$INSTDIR\Codex Monitor.exe" 0 legacy_done
  !include "${LEGACY_MANIFEST}"
  legacy_done:
  ; Remove only files the prior native installer shipped for its Electron widget.
  IfFileExists "$INSTDIR\widget\runtime\electron.exe" 0 widget_legacy_done
  !include "${LEGACY_WIDGET_MANIFEST}"
  widget_legacy_done:
  ClearErrors
  WriteUninstaller "$INSTDIR\Uninstall ${APP_NAME}.exe"
  ; 2.x shipped its uninstaller and shortcuts under the previous product name.
  ; The executable keeps its name so signed in-place updates still apply.
  !if "${APP_LEGACY_NAME}" != "${APP_NAME}"
  Delete "$INSTDIR\Uninstall ${APP_LEGACY_NAME}.exe"
  Delete "$DESKTOP\${APP_LEGACY_NAME}.lnk"
  Delete "$SMPROGRAMS\${APP_LEGACY_NAME}.lnk"
  !endif
  CreateShortcut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\icon.ico"
  CreateShortcut "$SMPROGRAMS\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\icon.ico"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP_NAME}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "${APP_NAME}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\icon.ico"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '$\"$INSTDIR\Uninstall ${APP_NAME}.exe$\"'
  WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '$\"$INSTDIR\Uninstall ${APP_NAME}.exe$\" /S'
  WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "https://github.com/oysterhyd/AgentMeter"
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "EstimatedSize" ${APP_SIZE_KB}
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
  IfErrors install_error
  SetErrorLevel 0
  Goto install_done
  install_error:
  MessageBox MB_OK|MB_ICONSTOP "$(InstallErrorText)" /SD IDOK
  SetErrorLevel 1
  Abort
  install_done:
SectionEnd

Section "Uninstall"
  Call un.CheckRunning
  ; Delete only files shipped by this installer and remove empty directories.
  !include "${UNINSTALL_MANIFEST}"
  Delete "$INSTDIR\Uninstall ${APP_NAME}.exe"
  Delete "$DESKTOP\${APP_NAME}.lnk"
  Delete "$SMPROGRAMS\${APP_NAME}.lnk"
  DeleteRegKey HKCU "${UNINSTALL_KEY}"
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APP_LOGIN_ID}"
  ${If} $0 == '$\"$INSTDIR\${APP_EXE}$\" --mygo-opened-at-login'
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APP_LOGIN_ID}"
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run" "${APP_LOGIN_ID}"
  ${EndIf}
  RMDir "$INSTDIR"
  SetErrorLevel 0
SectionEnd
