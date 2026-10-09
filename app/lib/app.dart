import 'dart:async';

import 'package:flutter/material.dart';

import 'data/admin.dart';
import 'data/api.dart';
import 'data/folders.dart';
import 'data/keys.dart';
import 'data/library.dart';
import 'data/pin.dart';
import 'data/platform.dart';
import 'data/server.dart';
import 'data/session.dart';
import 'data/zip.dart';
import 'l10n/app_localizations.dart';
import 'ui/first_start.dart';
import 'ui/home.dart';
import 'ui/invite.dart';
import 'ui/player.dart';
import 'ui/send/pin_entry_screen.dart';
import 'ui/send/send_screen.dart';
import 'ui/theme.dart';
import 'ui/zip/zip_open_screen.dart';
import 'ui/zip/zip_screen.dart';

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
    keys = KeysRepository(platform: platform, api: this.api);
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

  /// End-to-end encryption on this phone (docs/e2ee-plan.md).
  late final KeysRepository keys;

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
    // Signed out: the next person starts with their own folders.
    session.states.listen((s) {
      if (s is SignedOutState) unawaited(folders.clear());
    });
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

  /// The same, where a widget may be shown without them, as in a few tests.
  static AppServices? maybeRead(BuildContext context) => context.getInheritedWidgetOfExactType<Services>()?.services;

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
  StreamSubscription<void>? _zips;

  /// The keys may have news: someone to seal for or to ask about, a key sealed for this phone, a
  /// check to answer, a new version. So the app checks in when it comes back, and while it is in
  /// front: every half minute, and every few seconds while a check runs or this phone waits for
  /// keys (KeysState.pace).
  late final _lifecycle = AppLifecycleListener(onResume: () => unawaited(_checkInKeys()));
  Timer? _keysTimer;
  Duration _keysWait = Duration.zero;

  void _scheduleKeys() {
    _keysTimer?.cancel();
    _keysWait = widget.services.keys.state.pace;
    _keysTimer = Timer(_keysWait, () async {
      if (WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed) await _checkInKeys();
      if (mounted) _scheduleKeys();
    });
  }

  /// Sooner when the keys say so, e.g. once this phone turns out to wait for them.
  void _keysChanged() {
    if (widget.services.keys.state.pace < _keysWait) _scheduleKeys();
  }

  Future<void> _checkInKeys() async {
    final s = widget.services;
    if (s.session.current is SignedInState) await s.keys.checkIn();
  }

  @override
  void initState() {
    super.initState();
    _lifecycle;
    final s = widget.services;
    _scheduleKeys();
    s.keys.addListener(_keysChanged);
    unawaited(s.start());
    _links = s.platform.links.listen(_open);
    _shared = s.platform.sharedChanges.listen(_sharedChanged);
    unawaited(s.platform.initialLink().then((l) => l == null ? null : _open(l)));
    _zips = s.platform.zipArrivals.listen((_) => unawaited(s.platform.takeZip().then(_zip)));
    unawaited(s.platform.takeZip().then(_zip));
  }

  @override
  void dispose() {
    _links?.cancel();
    _shared?.cancel();
    _zips?.cancel();
    _lifecycle.dispose();
    _keysTimer?.cancel();
    widget.services.keys.removeListener(_keysChanged);
    super.dispose();
  }

  /// Files shared from another app that couldn't be taken, e.g. without room for a copy.
  void _sharedChanged(SharedFiles shared) {
    final messenger = _messenger.currentState;
    if (shared.skipped == 0 || messenger == null) return;
    messenger.showSnackBar(SnackBar(content: Text(AppLocalizations.of(messenger.context).sharedSkipped(shared.skipped))));
  }

  /// Another app's share sheet sent files to pack (Send as ZIP), or a ZIP was opened with Share: its
  /// screen opens over whatever is there, signed in or not (docs/zip-plan.md).
  void _zip(ZipArrival? arrival) {
    switch (arrival) {
      case final ZipToPack pack:
        _navigator.currentState?.push(MaterialPageRoute<void>(builder: (_) => ZipScreen(files: pack.files, skipped: pack.skipped, fromOutside: true)));
      case ZipToOpen():
        _navigator.currentState?.push(MaterialPageRoute<void>(builder: (_) => const ZipOpenScreen()));
      case null:
        break;
    }
  }

  /// A link that opened the app: an invite (from the invite page or a scan), or a PIN link.
  void _open(String url) {
    switch (parseLink(url)) {
      case final InviteLink link:
        _navigator.currentState?.push(MaterialPageRoute<void>(builder: (_) => InviteScreen(link: link)));
      case final PinLink link:
        _navigator.currentState?.push(MaterialPageRoute<void>(builder: (_) => PinEntryScreen(server: link.server, code: link.code, secret: link.secret, root: link.root)));
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
