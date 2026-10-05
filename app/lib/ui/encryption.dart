import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app.dart';
import '../data/api.dart';
import '../data/models.dart';
import '../data/platform.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import 'theme.dart';
import 'widgets.dart';

// End-to-end encryption in the app (docs/e2ee-plan.md): a folder's switch, the recovery code,
// and what a phone without its keys can do. The keys live in Kotlin (Platform's key calls).

String _keysProblem(AppLocalizations t, Object e) => switch (e) {
      KeysException(code: 'offline') || NetworkException() => t.commonOffline,
      _ => t.commonFailed,
    };

/// The recovery code, shown once: to write down or copy. Done only once it is kept.
Future<void> showRecoveryCode(BuildContext context, String code) => showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _RecoveryCodeDialog(code: code),
    );

class _RecoveryCodeDialog extends StatefulWidget {
  const _RecoveryCodeDialog({required this.code});
  final String code;

  @override
  State<_RecoveryCodeDialog> createState() => _RecoveryCodeDialogState();
}

class _RecoveryCodeDialogState extends State<_RecoveryCodeDialog> {
  bool _kept = false;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final groups = widget.code.split('-');
    return AlertDialog(
      icon: Icon(AppIcons.key, size: 28, color: c.accentText),
      title: Text(t.recoveryTitle),
      content: SingleChildScrollView(
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Text(t.recoveryLead, style: TextStyle(color: c.text2, height: 1.5)),
          const SizedBox(height: 16),
          Container(
            padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 12),
            decoration: BoxDecoration(color: c.s2, borderRadius: BorderRadius.circular(16)),
            child: SelectableText(
              '${groups.take(4).join('-')}\n${groups.skip(4).join('-')}',
              textAlign: TextAlign.center,
              style: const TextStyle(fontFamily: mono, fontSize: 18, fontWeight: FontWeight.w500, height: 1.6, letterSpacing: 1),
            ),
          ),
          const SizedBox(height: 8),
          Align(
            child: TextButton.icon(
              onPressed: () async {
                await Clipboard.setData(ClipboardData(text: widget.code));
                if (context.mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.recoveryCopied)));
              },
              icon: const Icon(AppIcons.copy, size: 18),
              label: Text(t.recoveryCopy),
            ),
          ),
          CheckboxListTile(
            value: _kept,
            onChanged: (v) => setState(() => _kept = v ?? false),
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            title: Text(t.recoveryKept, style: const TextStyle(fontSize: 15)),
          ),
        ]),
      ),
      actions: [
        TextButton(onPressed: _kept ? () => Navigator.pop(context) : null, child: Text(t.recoveryDone)),
      ],
    );
  }
}

/// Admins: opens every encrypted folder with the recovery code, e.g. on a new phone.
Future<void> useRecoveryCode(BuildContext context) => showDialog<void>(context: context, builder: (_) => const _UseRecoveryDialog());

class _UseRecoveryDialog extends StatefulWidget {
  const _UseRecoveryDialog();

  @override
  State<_UseRecoveryDialog> createState() => _UseRecoveryDialogState();
}

class _UseRecoveryDialogState extends State<_UseRecoveryDialog> {
  final _code = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _code.dispose();
    super.dispose();
  }

  bool get _complete => _code.text.replaceAll(RegExp(r'[-\s]'), '').length >= 32;

  Future<void> _use() async {
    final t = AppLocalizations.of(context);
    final services = Services.read(context);
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final n = await services.platform.useRecoveryCode(_code.text);
      unawaited(services.folders.load());
      navigator.pop();
      messenger.showSnackBar(SnackBar(content: Text(t.recoveryOpened(n))));
    } on KeysException catch (e) {
      if (mounted) setState(() => _error = e.sealed ? t.recoveryWrong : _keysProblem(t, e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return AlertDialog(
      title: Text(t.recoveryUseTitle),
      content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Text(t.recoveryUseLead, style: TextStyle(color: context.colors.text2, height: 1.5)),
        FieldLabel(t.recoveryCode),
        TextField(
          controller: _code,
          autofocus: true,
          autocorrect: false,
          enableSuggestions: false,
          textCapitalization: TextCapitalization.characters,
          style: const TextStyle(fontFamily: mono, letterSpacing: 0.5),
          decoration: InputDecoration(errorText: _error, errorMaxLines: 3),
          onChanged: (_) => setState(() => _error = null),
          onSubmitted: (_) => _complete && !_busy ? _use() : null,
        ),
      ]),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: Text(t.commonCancel)),
        TextButton(onPressed: _busy || !_complete ? null : _use, child: Text(t.recoveryUse)),
      ],
    );
  }
}

/// Over the library: a phone without its keys waits for another phone or browser of the person;
/// admins can use the recovery code, and anyone can start over.
class KeysBanner extends StatelessWidget {
  const KeysBanner({super.key, required this.admin});
  final bool admin;

  @override
  Widget build(BuildContext context) {
    final keys = Services.of(context).keys;
    return ListenableBuilder(
      listenable: keys,
      builder: (context, _) {
        if (!keys.waiting) return const SizedBox.shrink();
        final t = AppLocalizations.of(context);
        final c = context.colors;
        return Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
          child: Container(
            padding: const EdgeInsets.fromLTRB(14, 12, 14, 6),
            decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(16), border: Border.all(color: c.lineSoft)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Icon(AppIcons.lock, size: 20, color: c.accentText),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(t.keysWaitingTitle, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
                    const SizedBox(height: 3),
                    Text(t.keysWaiting, style: TextStyle(fontSize: 14, height: 1.45, color: c.text2)),
                  ]),
                ),
              ]),
              Wrap(alignment: WrapAlignment.end, spacing: 4, children: [
                if (admin) TextButton(onPressed: () => useRecoveryCode(context), child: Text(t.recoveryUse)),
                TextButton(onPressed: () => _startOver(context), child: Text(t.keysStartOver)),
              ]),
            ]),
          ),
        );
      },
    );
  }

  Future<void> _startOver(BuildContext context) async {
    final t = AppLocalizations.of(context);
    final services = Services.read(context);
    final messenger = ScaffoldMessenger.of(context);
    final sure = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        icon: Icon(AppIcons.key, size: 28, color: context.colors.accentText),
        title: Text(t.keysStartOverTitle),
        content: Text(t.keysStartOverBody, style: TextStyle(color: context.colors.text2, height: 1.5)),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
          TextButton(onPressed: () => Navigator.pop(context, true), child: Text(t.keysStartOver)),
        ],
      ),
    );
    if (sure != true) return;
    try {
      await services.platform.startOver();
    } on KeysException catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(_keysProblem(t, e))));
    }
  }
}

/// Admins, on a folder's page (screen 41): the switch for encrypting its new files. The first
/// encrypted folder makes the recovery key, whose code is shown once, before the folder's key.
/// With [ask], the question comes at once, as for a folder just made encrypted.
class EncryptionRow extends StatefulWidget {
  const EncryptionRow({super.key, required this.folder, this.ask = false});
  final FolderInfo folder;
  final bool ask;

  @override
  State<EncryptionRow> createState() => _EncryptionRowState();
}

class _EncryptionRowState extends State<EncryptionRow> {
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    if (widget.ask && !widget.folder.encrypted) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _askOn();
      });
    }
  }

  Future<bool> _confirm({required IconData icon, required String title, required String body, required String yes}) async =>
      await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          icon: Icon(icon, size: 28, color: context.colors.accentText),
          title: Text(title),
          content: Text(body, style: TextStyle(color: context.colors.text2, height: 1.5)),
          actions: [
            TextButton(onPressed: () => Navigator.pop(context, false), child: Text(AppLocalizations.of(context).commonCancel)),
            TextButton(onPressed: () => Navigator.pop(context, true), child: Text(yes)),
          ],
        ),
      ) ??
      false;

  Future<void> _askOn() async {
    final t = AppLocalizations.of(context);
    final services = Services.read(context);
    final messenger = ScaffoldMessenger.of(context);
    final f = widget.folder;
    // Until the keys are loaded, a recovery key there isn't known yet, and would be replaced.
    if (!services.keys.state.ready) {
      messenger.showSnackBar(SnackBar(content: Text(t.encryptionNoKeysHere)));
      return;
    }
    final first = !services.keys.state.hasRecovery;
    if (!await _confirm(icon: AppIcons.lock, title: t.encryptionAskTitle(f.name), body: first ? t.encryptionAskFirst : t.encryptionAskBody, yes: t.encryptionTurnOn) ||
        !mounted) {
      return;
    }
    setState(() => _busy = true);
    try {
      if (first) {
        final code = await services.platform.makeRecovery();
        if (!mounted) return;
        await showRecoveryCode(context, code);
      }
      await services.platform.encryptFolder(f);
      await services.folders.load();
      messenger.showSnackBar(SnackBar(content: Text(t.encryptionOn(f.name))));
    } on Exception catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(_keysProblem(t, e))));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _askOff() async {
    final t = AppLocalizations.of(context);
    final services = Services.read(context);
    final messenger = ScaffoldMessenger.of(context);
    final f = widget.folder;
    if (!await _confirm(icon: AppIcons.lockOpen, title: t.encryptionOffTitle(f.name), body: t.encryptionOffBody, yes: t.encryptionTurnOff) || !mounted) return;
    setState(() => _busy = true);
    try {
      await services.api.put('/api/folders/${f.id}/encryption', {'encrypted': false});
      await services.folders.load();
    } on Exception catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(_keysProblem(t, e))));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final keys = Services.of(context).keys;
    return ListenableBuilder(
      listenable: keys,
      builder: (context, _) {
        final ready = keys.state.ready;
        final on = widget.folder.encrypted;
        return SettingsRow(
          leading: SettingsRow.icon(context, AppIcons.lock),
          title: t.encryptionSwitch,
          subtitle: ready ? (on ? t.encryptionSwitchOn : t.encryptionSwitchOff) : t.encryptionNoKeysHere,
          trailing: Switch(value: on, onChanged: !ready || _busy ? null : (v) => v ? _askOn() : _askOff()),
        );
      },
    );
  }
}

/// Admins' settings for encryption: new folders encrypted or not, and the recovery code.
class EncryptionSettings extends StatefulWidget {
  const EncryptionSettings({super.key});

  @override
  State<EncryptionSettings> createState() => _EncryptionSettingsState();
}

class _EncryptionSettingsState extends State<EncryptionSettings> {
  bool? _newFolders;

  @override
  void initState() {
    super.initState();
    Services.read(context).keys.newFoldersEncrypted().then((on) {
      if (mounted) setState(() => _newFolders = on);
    }, onError: (Object _) {});
  }

  Future<void> _flip(bool on) async {
    final keys = Services.read(context).keys;
    final messenger = ScaffoldMessenger.of(context);
    final t = AppLocalizations.of(context);
    setState(() => _newFolders = on);
    try {
      await keys.setNewFoldersEncrypted(on);
    } on Exception {
      if (mounted) setState(() => _newFolders = !on);
      messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
    }
  }

  Future<void> _newCode() async {
    final t = AppLocalizations.of(context);
    final services = Services.read(context);
    final messenger = ScaffoldMessenger.of(context);
    final sure = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        icon: Icon(AppIcons.key, size: 28, color: context.colors.accentText),
        title: Text(t.recoveryNewTitle),
        content: Text(t.recoveryNewBody, style: TextStyle(color: context.colors.text2, height: 1.5)),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
          TextButton(onPressed: () => Navigator.pop(context, true), child: Text(t.recoveryNewButton)),
        ],
      ),
    );
    if (sure != true || !mounted) return;
    try {
      final code = await services.platform.makeRecovery();
      if (mounted) await showRecoveryCode(context, code);
    } on Exception catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(_keysProblem(t, e))));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final keys = Services.of(context).keys;
    return ListenableBuilder(
      listenable: keys,
      builder: (context, _) {
        final s = keys.state;
        return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          SectionLabel(t.encryptionTitle),
          SettingsGroup(children: [
            SettingsRow(
              leading: SettingsRow.icon(context, AppIcons.lock),
              title: t.encryptionNewFolders,
              subtitle: t.encryptionNewFoldersSub,
              trailing: Switch(value: _newFolders ?? false, onChanged: _newFolders == null ? null : _flip),
            ),
            SettingsRow(
              leading: SettingsRow.icon(context, AppIcons.key),
              title: t.recoveryTitle,
              subtitle: s.hasRecovery ? t.recoveryMade : t.recoveryNone,
              trailing: const SizedBox.shrink(),
              onTap: s.ready && s.hasRecovery ? _newCode : null,
            ),
            if (s.hasRecovery)
              SettingsRow(
                leading: SettingsRow.icon(context, AppIcons.lockOpen),
                title: t.recoveryUseTitle,
                trailing: const SizedBox.shrink(),
                onTap: () => useRecoveryCode(context),
              ),
          ]),
        ]);
      },
    );
  }
}
