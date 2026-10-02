import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../data/models.dart';
import '../../data/username.dart';
import '../../l10n/app_localizations.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

/// A new password for someone who forgot theirs, or never had one: first the username to sign
/// in with, then the password the server made up, shown only now. Their phones and browsers
/// stay signed in. Not for oneself: Settings › Password is for that, with the current one.
class NewPasswordDialog extends StatefulWidget {
  const NewPasswordDialog({super.key, required this.person});
  final Person person;

  @override
  State<NewPasswordDialog> createState() => _NewPasswordDialogState();
}

class _NewPasswordDialogState extends State<NewPasswordDialog> {
  late final _username = TextEditingController(text: widget.person.username ?? suggestedUsername(widget.person.name));
  ({String username, String password})? _made;
  String? _error;
  bool _busy = false;
  final _copied = <String>{};

  Future<void> _make() async {
    final t = AppLocalizations.of(context);
    if (!validUsername(_username.text)) return setState(() => _error = t.settingsUsernameBad);
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final made = await Services.read(context).admin.newPassword(widget.person.id, username: _username.text.trim());
      if (mounted) setState(() => _made = made);
    } on ApiException catch (e) {
      setState(() => _error = switch (e.code) {
            'username_taken' => t.settingsUsernameTaken,
            'bad_request' => t.settingsUsernameBad,
            _ => t.commonFailed,
          });
    } on NetworkException {
      setState(() => _error = t.commonOffline);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _copy(String what, String text, {bool secret = false}) async {
    if (secret) {
      await Services.read(context).platform.copySecret(text);
    } else {
      await Clipboard.setData(ClipboardData(text: text));
    }
    if (mounted) setState(() => _copied.add(what));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final p = widget.person;
    final made = _made;
    if (made == null) {
      return AlertDialog(
        title: Text(t.newPasswordTitle(p.name)),
        content: SingleChildScrollView(
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Text(t.newPasswordLead(p.name), style: TextStyle(fontSize: 15, height: 1.5, color: c.text2)),
            FieldLabel(t.signInUsername),
            TextField(
              controller: _username,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(errorText: _error, helperText: _error == null ? t.settingsUsernameBad : null, helperMaxLines: 2),
              onSubmitted: (_) => _make(),
            ),
          ]),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: Text(t.commonCancel)),
          TextButton(onPressed: _busy ? null : _make, child: Text(t.newPasswordMake)),
        ],
      );
    }
    final server = Services.read(context).api.config?.publicUrl.host ?? 'Share';
    return AlertDialog(
      title: Text(t.newPasswordTitle(p.name)),
      content: SingleChildScrollView(
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          _Shown(label: t.signInUsername, value: made.username, copied: _copied.contains('username'), onCopy: () => _copy('username', made.username)),
          const SizedBox(height: 10),
          _Shown(
            label: t.settingsPassword,
            value: made.password,
            copied: _copied.contains('password'),
            onCopy: () => _copy('password', made.password, secret: true),
          ),
          const SizedBox(height: 16),
          Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Icon(AppIcons.eyeOff, size: 18, color: c.text3),
            const SizedBox(width: 10),
            Expanded(child: Text(t.newPasswordShownOnce(p.name), style: TextStyle(fontSize: 14, height: 1.5, color: c.text2))),
          ]),
        ]),
      ),
      actions: [
        TextButton(
          onPressed: () => Services.read(context).platform.shareText(t.newPasswordShareText(server, made.username, made.password)),
          child: Text(t.newPasswordSend),
        ),
        TextButton(onPressed: () => Navigator.pop(context), child: Text(t.commonClose)),
      ],
    );
  }
}

/// A username or password, shown once, to copy.
class _Shown extends StatelessWidget {
  const _Shown({required this.label, required this.value, required this.copied, required this.onCopy});
  final String label, value;
  final bool copied;
  final VoidCallback onCopy;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 10, 4, 10),
      decoration: BoxDecoration(color: c.s2, borderRadius: BorderRadius.circular(14)),
      child: Row(children: [
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(label, style: TextStyle(fontSize: 12.5, color: c.text3)),
            const SizedBox(height: 2),
            SelectableText(value, style: const TextStyle(fontFamily: 'RobotoMono', fontSize: 17, fontWeight: FontWeight.w600)),
          ]),
        ),
        IconButton(
          tooltip: copied ? t.newPasswordCopied : label,
          icon: Icon(copied ? AppIcons.check : AppIcons.copy, size: 20, color: copied ? c.ok : c.text2),
          onPressed: onCopy,
        ),
      ]),
    );
  }
}
