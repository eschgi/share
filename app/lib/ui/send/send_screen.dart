import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/models.dart';
import '../../data/pin.dart';
import '../../data/platform.dart';
import '../../l10n/app_localizations.dart';
import '../about_screen.dart';
import '../folders.dart';
import '../format.dart';
import '../icons.dart';
import '../sign_in.dart';
import '../theme.dart';
import '../widgets.dart';
import 'pin_entry_screen.dart';
import 'send_panel.dart';
import 'shared.dart';

/// Screen 15 for someone signed in: no PIN needed. With a second folder, it says which folder
/// sending goes into (screen 48), and files shared from other apps wait there for the choice.
class SendScreen extends StatelessWidget {
  const SendScreen({super.key, required this.user, required this.navigation, required this.onAvatar});
  final User user;
  final Widget navigation;
  final VoidCallback onAvatar;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final services = Services.of(context);
    final folders = services.folders;
    return Scaffold(
      appBar: AppBar(
        titleSpacing: 22,
        title: Text(t.sendTitle),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 14),
            child: InkResponse(onTap: onAvatar, radius: 24, child: Avatar(name: user.name, id: user.id, size: 36)),
          ),
        ],
      ),
      bottomNavigationBar: navigation,
      body: ListenableBuilder(
        listenable: Listenable.merge([folders, services.shared]),
        builder: (context, _) => ListView(padding: const EdgeInsets.fromLTRB(16, 0, 16, 24), children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(6, 0, 6, 18),
            child: Text(t.sendLeadSignedIn, style: TextStyle(fontSize: 17, height: 1.5, color: context.colors.text2)),
          ),
          if (services.shared.value > 0 && folders.asksForFolder) ...[
            _WaitingShared(count: services.shared.value),
            const SizedBox(height: 16),
          ] else if (folders.list?.isEmpty ?? false) ...[
            NoteCard(icon: AppIcons.folder, text: t.sendNoFolder),
            const SizedBox(height: 16),
          ] else if (folders.choices) ...[
            FolderField(label: t.sendInto, value: folders.sendTo, onTap: () => chooseSendFolder(context)),
            const SizedBox(height: 20),
          ],
          const SendPanel(auth: SendAuth.device),
        ]),
      ),
    );
  }
}

/// Asks which folder sending goes into from now on; the folder, or null.
Future<String?> chooseSendFolder(BuildContext context) async {
  final folders = Services.read(context).folders;
  final id = await showFolderChoice(context, title: AppLocalizations.of(context).sendInto, list: folders.list ?? const [], value: folders.sendTo?.id);
  if (id != null) folders.chooseSendTo(id);
  return id;
}

/// Files shared from another app, waiting for the person to say which folder they go into.
class _WaitingShared extends StatelessWidget {
  const _WaitingShared({required this.count});
  final int count;

  Future<void> _send(BuildContext context, String folder) async {
    final services = Services.read(context);
    try {
      await services.platform.sendShared(SendAuth.device, folder: folder);
    } on Exception {
      if (context.mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(AppLocalizations.of(context).commonFailed)));
    }
    services.shared.value = await services.platform.sharedCount();
  }

  Future<void> _drop(BuildContext context) async {
    final services = Services.read(context);
    await services.platform.dropShared();
    services.shared.value = await services.platform.sharedCount();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final services = Services.of(context);
    final folders = services.folders;
    final to = folders.sendTo;
    final list = folders.list;
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 10),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(22), border: Border.all(color: c.lineSoft)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Row(children: [
          Icon(AppIcons.upload, size: 22, color: c.accentText),
          const SizedBox(width: 12),
          Expanded(child: Text(t.sendWaitingCount(count), style: const TextStyle(fontSize: 16.5, fontWeight: FontWeight.w600))),
        ]),
        const SizedBox(height: 16),
        if (list != null && list.isNotEmpty)
          FolderField(label: t.sendInto, value: to, onTap: () => chooseSendFolder(context))
        else if (list != null || folders.failed) // not while the folders are on their way
          Text(list == null ? t.commonOffline : t.sendNoFolder, style: TextStyle(fontSize: 15, height: 1.5, color: c.text2)),
        const SizedBox(height: 12),
        Wrap(alignment: WrapAlignment.end, spacing: 8, children: [
          TextButton(onPressed: () => _drop(context), child: Text(t.sharedDontSend)),
          if (to != null)
            FilledButton.icon(
              onPressed: () => _send(context, to.id),
              icon: const Icon(AppIcons.upload, size: 20),
              label: Text(t.sendWaitingSend(count)),
            ),
        ]),
      ]),
    );
  }
}

/// Screen 15 without an account: sending with a PIN, as on the website.
class PinSendScreen extends StatelessWidget {
  const PinSendScreen({super.key, required this.session});
  final PinSession session;

  void _newPin(BuildContext context) =>
      Navigator.push(context, MaterialPageRoute<void>(builder: (_) => PinEntryScreen(server: session.server)));

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final expires = session.expiresAt;
    // Files shared from another app go out with the PIN.
    return SharedSender(
      auth: SendAuth.pin,
      onShared: () => Navigator.of(context).popUntil((r) => r.isFirst),
      child: Scaffold(
        appBar: AppBar(
          titleSpacing: 22,
          title: Text(t.sendTitle),
          actions: [
            PopupMenuButton<String>(
              icon: const Icon(AppIcons.more),
              onSelected: (choice) async {
                if (choice == 'pin') return _newPin(context);
                final screen = choice == 'about' ? const AboutScreen(signedIn: false) : const SignInScreen();
                await Navigator.push(context, MaterialPageRoute<void>(builder: (_) => screen));
              },
              itemBuilder: (_) => [
                PopupMenuItem(value: 'pin', child: Text(t.pinOther)),
                PopupMenuItem(value: 'sign-in', child: Text(t.pinSignIn)),
                PopupMenuItem(value: 'about', child: Text(t.aboutTitle)),
              ],
            ),
          ],
        ),
        body: ListView(padding: const EdgeInsets.fromLTRB(16, 0, 16, 24), children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(6, 0, 6, 18),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(t.pinSendTo(session.server.host), style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
              if (session.kind == PinKind.day && expires != null) ...[
                const SizedBox(height: 4),
                Text(t.pinValidUntil(formatWhen(expires, clock.now(), locale)), style: TextStyle(fontSize: 14.5, color: c.warn)),
              ],
              const SizedBox(height: 8),
              Text(t.pinSendLead, style: TextStyle(fontSize: 16, height: 1.5, color: c.text2)),
            ]),
          ),
          if (session.ended) ...[
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
              child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                Text(t.sendPinEnded, style: TextStyle(fontSize: 15.5, height: 1.5, color: c.text2)),
                const SizedBox(height: 12),
                FilledButton(onPressed: () => _newPin(context), child: Text(t.sendNewPin)),
              ]),
            ),
            const SizedBox(height: 16),
          ],
          SendPanel(auth: SendAuth.pin, onNewPin: () => _newPin(context)),
        ]),
      ),
    );
  }
}
