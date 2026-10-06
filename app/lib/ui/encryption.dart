import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'package:clock/clock.dart';

import '../app.dart';
import '../data/api.dart';
import '../data/models.dart';
import '../data/platform.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import 'theme.dart';
import 'widgets.dart';

// End-to-end encryption in the app (docs/e2ee-plan.md): a folder's switch, the recovery code,
// what a phone without its keys can do, and the checks before keys are passed on. The keys live
// in Kotlin (Platform's key calls).

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

/// A check's code, in two groups of three, as the other screen shows it.
class CheckCode extends StatelessWidget {
  const CheckCode(this.code, {super.key});
  final String code;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    Widget digit(String d) => Container(
          width: 38,
          height: 46,
          margin: const EdgeInsets.only(right: 6),
          alignment: Alignment.center,
          decoration: BoxDecoration(color: c.s2, borderRadius: BorderRadius.circular(12), border: Border.all(color: c.line)),
          child: Text(d, style: TextStyle(fontFamily: mono, fontSize: 21, fontWeight: FontWeight.w600, color: c.text)),
        );
    return Semantics(
      label: '${code.substring(0, 3)} ${code.substring(3)}',
      excludeSemantics: true,
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        for (final d in code.substring(0, 3).split('')) digit(d),
        const SizedBox(width: 10),
        for (final d in code.substring(3).split('')) digit(d),
      ]),
    );
  }
}

/// The codes this phone shows: one, or one per phone or browser that asks, with its name.
class _ShownCodes extends StatelessWidget {
  const _ShownCodes({required this.codes});
  final List<ShownCode> codes;

  @override
  Widget build(BuildContext context) {
    if (codes.length == 1) return CheckCode(codes.single.code);
    return Wrap(spacing: 24, runSpacing: 10, children: [
      for (final x in codes)
        Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
          Text(x.from, style: TextStyle(fontSize: 13, color: context.colors.text2)),
          const SizedBox(height: 4),
          CheckCode(x.code),
        ]),
    ]);
  }
}

/// What a phone without its keys says, over the library (screens 50 and 55): it waits until one
/// of the person's phones or browsers allows it, with the code that one shows too, or for an
/// admin to allow the person's new key; admins can use the recovery code; and anyone can start
/// over.
class KeysBanner extends StatelessWidget {
  const KeysBanner({super.key, required this.admin});
  final bool admin;

  @override
  Widget build(BuildContext context) {
    final keys = Services.of(context).keys;
    return ListenableBuilder(
      listenable: keys,
      builder: (context, _) {
        final waiting = keys.waiting;
        final later = _waitingList(context, keys.state.asks);
        if (!waiting && !keys.waitsForFolders) return later;
        final t = AppLocalizations.of(context);
        final c = context.colors;
        final codes = [for (final x in keys.state.codes) if (x.kind == (waiting ? 'device' : 'person')) x];
        final text = waiting ? [t.keysWaiting, if (codes.isNotEmpty) t.keysSameCode].join(' ') : (codes.isEmpty ? t.keysAdmin : t.keysAdminCode);
        final banner = Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
          child: Container(
            padding: EdgeInsets.fromLTRB(14, 12, 14, admin || waiting ? 6 : 14),
            decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(16), border: Border.all(color: c.lineSoft)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Icon(AppIcons.lock, size: 20, color: c.accentText),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(waiting ? t.keysWaitingTitle : t.keysAdminTitle, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
                    const SizedBox(height: 3),
                    Text(text, style: TextStyle(fontSize: 14, height: 1.45, color: c.text2)),
                    if (codes.isNotEmpty) ...[const SizedBox(height: 10), _ShownCodes(codes: codes)],
                  ]),
                ),
              ]),
              if (admin || waiting)
                Wrap(alignment: WrapAlignment.end, spacing: 4, children: [
                  if (admin) TextButton(onPressed: () => useRecoveryCode(context), child: Text(t.recoveryUse)),
                  if (waiting) TextButton(onPressed: () => _startOver(context), child: Text(t.keysStartOver)),
                ]),
            ]),
          ),
        );
        return Column(children: [banner, later]);
      },
    );
  }

  /// Who waits for this phone's OK (screens 51 and 52): a new phone or browser of the person, or
  /// another person and their folders. Nothing opens by itself: Show opens the sheet, which starts
  /// the check, and Not now closes it again; the ask stays listed while it is due.
  Widget _waitingList(BuildContext context, List<KeyAsk> later) {
    if (later.isEmpty) return const SizedBox.shrink();
    final t = AppLocalizations.of(context);
    final c = context.colors;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
      child: Container(
        padding: const EdgeInsets.fromLTRB(14, 12, 6, 4),
        decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(16), border: Border.all(color: c.lineSoft)),
        child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Icon(AppIcons.key, size: 20, color: c.accentText),
          const SizedBox(width: 12),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(t.keysLaterTitle, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
              for (final x in later)
                Row(children: [
                  Expanded(
                    child: Text(
                      x.isPerson ? t.keysPersonTitle(x.name) : '${x.isBrowser ? t.keysAskBrowser : t.keysAskPhone}: ${x.name}',
                      style: TextStyle(fontSize: 14, height: 1.45, color: c.text2),
                    ),
                  ),
                  TextButton(onPressed: () => _show(context, x), child: Text(t.keysShow)),
                ]),
            ]),
          ),
        ]),
      ),
    );
  }

  /// Show: the sheet opens at once, waiting for the code, while the check starts; closed without
  /// an answer, the check ends.
  Future<void> _show(BuildContext context, KeyAsk ask) async {
    final t = AppLocalizations.of(context);
    final keys = Services.read(context).keys;
    final messenger = ScaffoldMessenger.of(context);
    unawaited(keys.show(ask).catchError((Object e) {
      messenger.showSnackBar(SnackBar(content: Text(_keysProblem(t, e))));
    }, test: (e) => e is KeysException));
    final settled = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      builder: (_) => KeyAskSheet(kind: ask.kind, id: ask.id),
    );
    if (settled != true) await keys.hide(ask);
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

/// The sheet Show opens: a new phone or browser of the person (51), or another person's new key
/// (52), with the code once the other side answered. It closes by itself once the ask is gone,
/// e.g. allowed on another phone; true when it was settled (Allow, Not me, Not now).
class KeyAskSheet extends StatefulWidget {
  const KeyAskSheet({super.key, required this.kind, required this.id});
  final String kind;
  final String id;

  @override
  State<KeyAskSheet> createState() => _KeyAskSheetState();
}

class _KeyAskSheetState extends State<KeyAskSheet> {
  KeyAsk? _last;
  bool _busy = false;
  bool _gone = false;
  String? _problem;

  Future<void> _act(Future<void> Function() run) async {
    setState(() {
      _busy = true;
      _problem = null;
    });
    try {
      await run();
      _close();
    } on KeysException catch (e) {
      if (mounted) setState(() => _problem = _keysProblem(AppLocalizations.of(context), e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Closes the sheet, settled, once: only while it is in front, as a sheet that the person
  /// closed already is gone from the navigator.
  void _close() {
    if (_gone || !mounted) return;
    _gone = true;
    if (ModalRoute.of(context)?.isCurrent ?? false) Navigator.pop(context, true);
  }

  /// "Signed in just now", "… 5 minutes ago", "… 2 hours ago", "… 3 days ago".
  static String _signedIn(AppLocalizations t, DateTime since) {
    final minutes = clock.now().difference(since).inMinutes;
    if (minutes < 60) return t.keysSignedInMinutes(minutes < 0 ? 0 : minutes);
    if (minutes < 24 * 60) return t.keysSignedInHours(minutes ~/ 60);
    return t.keysSignedInDays(minutes ~/ (24 * 60));
  }

  @override
  Widget build(BuildContext context) {
    final services = Services.of(context);
    final keys = services.keys;
    return ListenableBuilder(
      listenable: Listenable.merge([keys, services.folders]),
      builder: (context, _) {
        final now = keys.state.asks.where((a) => a.kind == widget.kind && a.id == widget.id).firstOrNull;
        if (now != null) _last = now;
        final ask = _last;
        if ((now == null || ask == null) && !_busy && !_gone) WidgetsBinding.instance.addPostFrameCallback((_) => _close());
        if (ask == null) return const SizedBox(height: 120);
        final t = AppLocalizations.of(context);
        final c = context.colors;
        final code = ask.code;
        final browser = ask.isBrowser;
        return SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(20, 4, 20, 12),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
              Text(ask.isPerson ? t.keysPersonTitle(ask.name) : (browser ? t.keysAskBrowser : t.keysAskPhone), style: Theme.of(context).textTheme.headlineSmall),
              const SizedBox(height: 14),
              if (ask.isPerson) ...[
                Text(ask.keyChanged ? t.keysPersonNew(ask.name) : t.keysPersonFirst(ask.name), style: TextStyle(fontSize: 15, color: c.text2, height: 1.5)),
                const SizedBox(height: 12),
                Wrap(spacing: 8, runSpacing: 8, children: [
                  for (final id in ask.folders) _FolderTag(name: services.folders.byId(id)?.name ?? '…'),
                ]),
              ] else
                SettingsGroup(children: [
                  SettingsRow(
                    leading: SettingsRow.icon(context, browser ? AppIcons.monitor : AppIcons.smartphone, accent: true),
                    title: ask.name,
                    subtitle: ask.since == null ? null : _signedIn(t, ask.since!),
                  ),
                ]),
              const SizedBox(height: 22),
              Text(ask.isPerson ? t.keysPersonCode(ask.name) : (browser ? t.keysShowsBrowser : t.keysShowsPhone),
                  style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w500)),
              const SizedBox(height: 8),
              Align(
                alignment: Alignment.centerLeft,
                child: code == null
                    ? SizedBox(height: 46, child: Align(alignment: Alignment.centerLeft, child: Text(t.keysWaitCode, style: TextStyle(fontSize: 15, color: c.text3))))
                    : CheckCode(code),
              ),
              const SizedBox(height: 12),
              Markup(ask.isPerson ? t.keysPersonHelp : (browser ? t.keysNotMeBrowser : t.keysNotMePhone), style: TextStyle(fontSize: 14, color: c.text3, height: 1.5)),
              if (_problem != null) ...[
                const SizedBox(height: 10),
                Text(_problem!, style: TextStyle(fontSize: 14, color: c.danger)),
              ],
              const SizedBox(height: 22),
              Row(children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: _busy ? null : () => _act(() => ask.isPerson ? keys.hide(ask) : keys.deny(ask)),
                    child: Text(ask.isPerson ? t.keysNotNow : t.keysNotMe),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: FilledButton.icon(
                    onPressed: _busy || code == null ? null : () => _act(() => keys.allow(ask)),
                    icon: const Icon(AppIcons.check, size: 20),
                    label: Text(t.keysAllow),
                  ),
                ),
              ]),
            ]),
          ),
        );
      },
    );
  }
}

/// A folder a person waits for, on the sheet that asks about them.
class _FolderTag extends StatelessWidget {
  const _FolderTag({required this.name});
  final String name;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      height: 28,
      padding: const EdgeInsets.symmetric(horizontal: 10),
      decoration: BoxDecoration(color: c.s2, borderRadius: BorderRadius.circular(14)),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        Icon(AppIcons.folder, size: 14, color: c.text2),
        const SizedBox(width: 6),
        Flexible(
          child: Text(name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: c.text2)),
        ),
      ]),
    );
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
