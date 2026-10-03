import 'dart:async';

import 'package:flutter/material.dart';

import 'data/admin.dart';
import 'data/api.dart';
import 'data/folders.dart';
import 'data/library.dart';
import 'data/pin.dart';
import 'data/platform.dart';
import 'data/server.dart';
import 'data/session.dart';
import 'l10n/app_localizations.dart';
import 'ui/first_start.dart';
import 'ui/home.dart';
import 'ui/invite.dart';
import 'ui/player.dart';
import 'ui/send/pin_entry_screen.dart';
import 'ui/send/send_screen.dart';
import 'ui/theme.dart';

/// Everything the screens use, made once. Tests build it with a fake platform and a mock
/// HTTP client.
class AppServices {
  AppServices({required this.platform, Api? api, MediaPlayerFactory? player})
      : api = api ?? Api(platform: platform),
        player = player ?? videoPlayer {
    session = SessionRepository(api: this.api, platform: platform);
    library = LibraryRepository(api: this.api, platform: platform);
    folders = FolderStore(library: library, platform: platform);
    admin = AdminRepository(api: this.api);
    pin = PinRepository(api: this.api, platform: platform);
  }

  final Platform platform;
  final Api api;

  /// Plays videos and sound in the viewer.
  final MediaPlayerFactory player;
  late final SessionRepository session;
  late final LibraryRepository library;
  late final FolderStore folders;
  late final AdminRepository admin;
  late final PinRepository pin;

  /// The language the person picked, or null for the phone's.
  final language = ValueNotifier<String?>(null);

  /// The theme the person picked (ThemeChoice), or null for Ember.
  final theme = ValueNotifier<String?>(null);

  /// How many files shared from other apps wait to be sent (SharedSender).
  final shared = ValueNotifier<int>(0);

  Future<void> start() async {
    platform.sharedChanges.listen((s) => shared.value = s.count);
    shared.value = await platform.sharedCount();
    theme.value = await platform.readSecret('theme');
    language.value = await platform.readSecret('language');
    await folders.start();
    await session.restore();
    await pin.restore();
  }

  Future<void> setLanguage(String? code) async {
    language.value = code;
    await platform.writeSecret('language', code);
  }

  Future<void> setTheme(String? choice) async {
    theme.value = choice;
    await platform.writeSecret('theme', choice);
  }
}

class Services extends InheritedWidget {
  const Services({super.key, required this.services, required super.child});
  final AppServices services;

  static AppServices of(BuildContext context) => context.dependOnInheritedWidgetOfExactType<Services>()!.services;

  /// For one-off reads, e.g. in initState or callbacks.
  static AppServices read(BuildContext context) => context.getInheritedWidgetOfExactType<Services>()!.services;

  @override
  bool updateShouldNotify(Services old) => old.services != services;
}

class ShareApp extends StatefulWidget {
  const ShareApp({super.key, required this.services});
  final AppServices services;

  @override
  State<ShareApp> createState() => _ShareAppState();
}

class _ShareAppState extends State<ShareApp> {
  final _navigator = GlobalKey<NavigatorState>();
  final _messenger = GlobalKey<ScaffoldMessengerState>();
  StreamSubscription<String>? _links;
  StreamSubscription<SharedFiles>? _shared;

  @override
  void initState() {
    super.initState();
    final s = widget.services;
    unawaited(s.start());
    _links = s.platform.links.listen(_open);
    _shared = s.platform.sharedChanges.listen(_sharedChanged);
    unawaited(s.platform.initialLink().then((l) => l == null ? null : _open(l)));
  }

  @override
  void dispose() {
    _links?.cancel();
    _shared?.cancel();
    super.dispose();
  }

  /// Files shared from another app that couldn't be taken, e.g. without room for a copy.
  void _sharedChanged(SharedFiles shared) {
    final messenger = _messenger.currentState;
    if (shared.skipped == 0 || messenger == null) return;
    messenger.showSnackBar(SnackBar(content: Text(AppLocalizations.of(messenger.context).sharedSkipped(shared.skipped))));
  }

  /// A link that opened the app: an invite (from the invite page or a scan), or a PIN link.
  void _open(String url) {
    switch (parseLink(url)) {
      case final InviteLink link:
        _navigator.currentState?.push(MaterialPageRoute<void>(builder: (_) => InviteScreen(link: link)));
      case final PinLink link:
        _navigator.currentState?.push(MaterialPageRoute<void>(builder: (_) => PinEntryScreen(server: link.server, code: link.code)));
      case null:
        break;
    }
  }

  @override
  Widget build(BuildContext context) => Services(
        services: widget.services,
        child: ListenableBuilder(
          listenable: Listenable.merge([widget.services.language, widget.services.theme]),
          builder: (context, _) {
            final language = widget.services.language.value;
            final (light, dark, mode) = ThemeChoice.resolve(widget.services.theme.value);
            return MaterialApp(
              navigatorKey: _navigator,
              scaffoldMessengerKey: _messenger,
              onGenerateTitle: (_) => 'Share',
              debugShowCheckedModeBanner: false,
              theme: shareTheme(light),
              darkTheme: shareTheme(dark),
              themeMode: mode,
              locale: language == null ? null : Locale(language),
              supportedLocales: AppLocalizations.supportedLocales,
              localizationsDelegates: AppLocalizations.localizationsDelegates,
              home: const SessionGate(),
            );
          },
        ),
      );
}

/// Signed out: the first screen. Signed in: the app.
class SessionGate extends StatelessWidget {
  const SessionGate({super.key});

  @override
  Widget build(BuildContext context) {
    final session = Services.of(context).session;
    return StreamBuilder<SessionState>(
      stream: session.states,
      initialData: session.current,
      builder: (context, snap) => switch (snap.data!) {
        SessionLoading() => const Scaffold(),
        SignedOutState(:final byServer) => StreamBuilder<PinSession?>(
            stream: Services.of(context).pin.states,
            initialData: Services.of(context).pin.current,
            builder: (context, pin) => pin.data == null
                ? FirstStartScreen(signedOutByServer: byServer)
                : PinSendScreen(session: pin.data!),
          ),
        SignedInState(:final user) => HomeShell(user: user),
      },
    );
  }
}
