import 'package:flutter/material.dart';

import '../app.dart';
import '../data/platform.dart';
import '../data/server.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import 'invite.dart';
import 'send/pin_entry_screen.dart';
import 'sign_in.dart';
import 'theme.dart';
import 'widgets.dart';

/// Screen 7: two doors. Sending needs only a PIN, seeing needs an account or an invite.
class FirstStartScreen extends StatelessWidget {
  const FirstStartScreen({super.key, this.signedOutByServer = false});
  final bool signedOutByServer;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      body: SafeArea(
        child: LayoutBuilder(
          builder: (context, box) => SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(24, 12, 24, 16),
            child: ConstrainedBox(
              constraints: BoxConstraints(minHeight: box.maxHeight - 28),
              child: IntrinsicHeight(
                child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                  const Padding(padding: EdgeInsets.symmetric(vertical: 12), child: Brand(name: 'Share')),
                  if (signedOutByServer) ...[
                    const SizedBox(height: 12),
                    NoteCard(icon: AppIcons.logOut, text: t.commonSignedOut),
                  ],
                  const _SharedWaiting(),
                  const SizedBox(height: 14),
                  const PhotoStack(left: 3, right: 2),
                  const SizedBox(height: 26),
                  HeroTitle(t.firstStartTitle),
                  const SizedBox(height: 22),
                  ChoiceCard(
                    icon: AppIcons.upload,
                    title: t.firstStartSend,
                    detail: t.firstStartSendDetail,
                    onTap: () => Navigator.push(context, MaterialPageRoute<void>(builder: (_) => const SendServerScreen())),
                  ),
                  const SizedBox(height: 14),
                  ChoiceCard(
                    icon: AppIcons.images,
                    title: t.firstStartSee,
                    detail: t.firstStartSeeDetail,
                    onTap: () => Navigator.push(context, MaterialPageRoute<void>(builder: (_) => const SignInScreen())),
                  ),
                  const Spacer(),
                  const SizedBox(height: 16),
                  TextButton.icon(
                    onPressed: () => scanInvite(context),
                    icon: const Icon(AppIcons.scan, size: 20),
                    label: Text(t.firstStartScan),
                  ),
                ]),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Files shared from another app, waiting for a sign-in or a PIN; or they can go.
class _SharedWaiting extends StatelessWidget {
  const _SharedWaiting();

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final services = Services.of(context);
    return ValueListenableBuilder<int>(
      valueListenable: services.shared,
      builder: (context, count, _) => count == 0
          ? const SizedBox()
          : Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Column(crossAxisAlignment: CrossAxisAlignment.end, children: [
                NoteCard(icon: AppIcons.upload, text: t.sharedWaiting(count)),
                TextButton(
                  onPressed: () async {
                    await services.platform.dropShared();
                    services.shared.value = 0;
                  },
                  child: Text(t.sharedDontSend),
                ),
              ]),
            ),
    );
  }
}

/// Scans an invite's QR code, or, without Google Play services, asks for the link, and opens
/// what it was: an invite (screen 10), or a PIN link (sending with that PIN, screen 1).
Future<void> scanInvite(BuildContext context) async {
  final services = Services.read(context);
  final t = AppLocalizations.of(context);
  final messenger = ScaffoldMessenger.of(context);
  String? text;
  try {
    text = await services.platform.scanCode();
  } on ScanUnavailable {
    if (!context.mounted) return;
    text = await askForInviteLink(context);
  }
  if (text == null || !context.mounted) return;
  switch (parseLink(text)) {
    case final InviteLink link:
      await Navigator.push(context, MaterialPageRoute<void>(builder: (_) => InviteScreen(link: link)));
    case final PinLink link:
      await Navigator.push(context, MaterialPageRoute<void>(builder: (_) => PinEntryScreen(server: link.server, code: link.code)));
    case null:
      messenger.showSnackBar(SnackBar(content: Text(t.scanNotAnInvite)));
  }
}

Future<String?> askForInviteLink(BuildContext context) {
  final t = AppLocalizations.of(context);
  final field = TextEditingController();
  return showDialog<String>(
    context: context,
    builder: (context) => AlertDialog(
      title: Text(t.pasteInvite),
      content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(t.scanUnavailable, style: TextStyle(color: context.colors.text2)),
        const SizedBox(height: 16),
        TextField(controller: field, autofocus: true, decoration: InputDecoration(hintText: t.pasteInviteHint)),
      ]),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: Text(t.commonCancel)),
        TextButton(onPressed: () => Navigator.pop(context, field.text), child: Text(t.commonSave)),
      ],
    ),
  );
}

/// Sending without an account, with a PIN: first where to.
class SendServerScreen extends StatefulWidget {
  const SendServerScreen({super.key});

  @override
  State<SendServerScreen> createState() => _SendServerScreenState();
}

class _SendServerScreenState extends State<SendServerScreen> {
  final _address = TextEditingController();
  String? _error;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(leading: const ShareBackButton()),
      body: SafeArea(
        child: ListView(padding: const EdgeInsets.fromLTRB(24, 4, 24, 24), children: [
          HeroTitle(t.sendServerTitle),
          Lead(t.sendServerLead),
          FieldLabel(t.signInServer),
          TextField(
            controller: _address,
            keyboardType: TextInputType.url,
            autocorrect: false,
            decoration: InputDecoration(hintText: t.signInServerHint, errorText: _error),
            onSubmitted: (_) => _open(),
          ),
          const SizedBox(height: 24),
          FilledButton(onPressed: _open, child: Text(t.sendServerOpen)),
        ]),
      ),
    );
  }

  void _open() {
    final server = normalizePublicAddress(_address.text);
    if (server == null) {
      setState(() => _error = AppLocalizations.of(context).serverBadAddress);
      return;
    }
    Navigator.push(context, MaterialPageRoute<void>(builder: (_) => PinEntryScreen(server: server)));
  }
}
