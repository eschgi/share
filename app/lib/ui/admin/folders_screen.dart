import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../data/folders.dart';
import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../encryption.dart';
import '../folders.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';
import 'pins_screen.dart';

/// What went wrong with a change to a folder, in words.
String folderProblem(AppLocalizations t, Object e) => switch (e) {
      ApiException(code: 'folder_name_taken') => t.folderNameTaken,
      ApiException(code: 'bad_request') => t.folderNameBad,
      ApiException(code: 'folder_busy') => t.folderBusy,
      ApiException(code: 'last_folder') => t.folderLastFolder,
      ApiException(code: 'not_found') => t.folderGone,
      NetworkException() => t.commonOffline,
      _ => t.commonFailed,
    };

const _titleStyle = TextStyle(fontFamily: 'Roboto', fontSize: 20, fontWeight: FontWeight.w600);

/// Screen 40: every folder, with what it holds, who sees it and whether a PIN sends into it.
class FoldersScreen extends StatefulWidget {
  const FoldersScreen({super.key});

  @override
  State<FoldersScreen> createState() => _FoldersScreenState();
}

class _FoldersScreenState extends State<FoldersScreen> {
  late final AppServices _services = Services.read(context);
  People? _people;
  List<PinInfo>? _pins;

  @override
  void initState() {
    super.initState();
    _services.folders.addListener(_changed);
    _load();
  }

  @override
  void dispose() {
    _services.folders.removeListener(_changed);
    super.dispose();
  }

  void _changed() => setState(() {});

  Future<void> _load() async {
    final admin = _services.admin;
    Future<void> load<T>(Future<T> f, void Function(T) keep) => f.then(keep).catchError((Object _) {});
    await Future.wait([
      _services.folders.load(),
      load(admin.people(), (v) => _people = v),
      load(admin.pins(), (v) => _pins = v),
    ]);
    if (mounted) setState(() {});
  }

  Future<void> _open(String id, {bool encrypt = false}) async {
    await Navigator.push(context, MaterialPageRoute<void>(builder: (_) => FolderScreen(id: id, encrypt: encrypt)));
    await _load();
  }

  Future<void> _new() async {
    final made = await showDialog<FolderName>(context: context, builder: (_) => const FolderNameDialog());
    // Encrypting it asks once more, and the first time makes the recovery code.
    if (made != null && mounted) await _open(made.folder.id, encrypt: made.encrypt);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final lines = FolderLines(context);
    final list = _services.folders.list;
    final people = _people;
    return Scaffold(
      appBar: AppBar(leading: const ShareBackButton(), title: Text(t.foldersTitle, style: _titleStyle)),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _new,
        icon: const Icon(AppIcons.plus, size: 26),
        label: Text(t.foldersNew, style: const TextStyle(fontFamily: 'Roboto', fontSize: 17, fontWeight: FontWeight.w600)),
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(padding: const EdgeInsets.fromLTRB(16, 4, 16, 110), children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(4, 0, 4, 16),
            child: Text(t.foldersLead, style: TextStyle(fontSize: 16.5, height: 1.55, color: c.text2)),
          ),
          if (list == null && !_services.folders.failed) const Padding(padding: EdgeInsets.only(top: 80), child: Center(child: CircularProgressIndicator())),
          if (list == null && _services.folders.failed) Help(t.commonOffline),
          if (list != null)
            SettingsGroup(children: [
              for (final f in list)
                SettingsRow(
                  leading: FolderCover(folder: f),
                  title: f.name,
                  subtitle: [
                    if (f.encrypted) t.encryptionEncrypted,
                    lines.count(f.files),
                    if (_pins?.where((p) => p.folder == f.id).length case final n? when n > 0) t.foldersPins(n),
                    if (f.adminsOnly) t.folderOnlyAdminsLine,
                  ].join(' · '),
                  trailing: Row(mainAxisSize: MainAxisSize.min, children: [
                    if (people != null) AvatarStack(people: whoSees(people, f.id)),
                    const SizedBox(width: 6),
                    Icon(AppIcons.chevronRight, size: 18, color: c.text3),
                  ]),
                  onTap: () => _open(f.id),
                ),
            ]),
        ]),
      ),
    );
  }
}

/// Screen 41: a folder, whether its files are encrypted, who sees it, the PINs that send into
/// it, and renaming or deleting it. With [encrypt], encrypting it is asked at once.
class FolderScreen extends StatefulWidget {
  const FolderScreen({super.key, required this.id, this.encrypt = false});
  final String id;
  final bool encrypt;

  @override
  State<FolderScreen> createState() => _FolderScreenState();
}

class _FolderScreenState extends State<FolderScreen> {
  late final AppServices _services = Services.read(context);
  People? _people;
  List<PinInfo>? _pins;

  /// Switches turned already, before the server's answer.
  final _turned = <String, bool>{};

  @override
  void initState() {
    super.initState();
    _services.folders.addListener(_changed);
    _load();
  }

  @override
  void dispose() {
    _services.folders.removeListener(_changed);
    super.dispose();
  }

  void _changed() => setState(() {});

  void _say(String text) => ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));

  Future<void> _load() async {
    final admin = _services.admin;
    Future<void> load<T>(Future<T> f, void Function(T) keep) => f.then(keep).catchError((Object _) {});
    await Future.wait([
      load(admin.people(), (v) => _people = v),
      load(admin.pins(), (v) => _pins = v),
    ]);
    if (mounted) setState(() {});
  }

  /// Gives someone the folder, or takes it away; the switch moves at once.
  Future<void> _turn(String key, bool sees, Future<void> Function() change) async {
    final t = AppLocalizations.of(context);
    setState(() => _turned[key] = sees);
    try {
      await change();
      await Future.wait([_load(), _services.folders.load()]);
    } on Exception catch (e) {
      _say(folderProblem(t, e));
    }
    if (mounted) setState(() => _turned.remove(key));
  }

  Future<void> _rename(FolderInfo f) => showDialog<FolderName>(context: context, builder: (_) => FolderNameDialog(folder: f));

  Future<void> _delete(FolderInfo f) async {
    final t = AppLocalizations.of(context);
    final navigator = Navigator.of(context);
    if (!await _confirmDelete(f) || !mounted) return;
    try {
      await _services.admin.deleteFolder(f.id);
    } on Exception catch (e) {
      return _say(folderProblem(t, e));
    }
    _say(t.folderDeleted(f.name));
    navigator.pop();
    await _services.folders.load();
  }

  Future<bool> _confirmDelete(FolderInfo f) async {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    return await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            icon: Icon(AppIcons.trash, size: 28, color: c.danger),
            title: Text(t.folderDeleteTitle(f.name)),
            content: Text(t.folderDeleteBody, style: TextStyle(color: c.text2, height: 1.5)),
            actions: [
              TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
              TextButton(
                style: TextButton.styleFrom(foregroundColor: c.danger),
                onPressed: () => Navigator.pop(context, true),
                child: Text(t.folderDelete),
              ),
            ],
          ),
        ) ??
        false;
  }

  /// A PIN that sends into the folder, with what can be done with it.
  Future<void> _pin(PinInfo p) => showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        builder: (sheet) => SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: PinCard(
              pin: p,
              onChanged: () {
                Navigator.pop(sheet);
                _load();
              },
            ),
          ),
        ),
      );

  Future<void> _newPin() async {
    if (await makePin(context, folder: widget.id) != null) await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final now = clock.now();
    final folders = _services.folders;
    final f = folders.byId(widget.id);
    final people = _people;
    if (f == null) {
      return Scaffold(
        appBar: AppBar(leading: const ShareBackButton()),
        body: Padding(padding: const EdgeInsets.all(20), child: Help(folders.list == null ? t.commonOffline : t.folderGone)),
      );
    }
    Widget sees(String key, {required bool admin, required bool given, required Future<void> Function(bool) set}) {
      final on = admin || (_turned[key] ?? given);
      return Switch(value: on, onChanged: admin ? null : (v) => _turn(key, v, () => set(v)));
    }

    final sending = [...?_pins?.where((p) => p.folder == f.id)];
    return Scaffold(
      appBar: AppBar(
        leading: const ShareBackButton(),
        title: Text(f.name, style: _titleStyle),
        actions: [
          PopupMenuButton<VoidCallback>(
            icon: const Icon(AppIcons.more),
            onSelected: (action) => action(),
            itemBuilder: (_) => [
              PopupMenuItem(value: () => _rename(f), child: Text(t.folderRename)),
              if ((folders.list?.length ?? 0) > 1) PopupMenuItem(value: () => _delete(f), child: Text(t.folderDelete, style: TextStyle(color: c.danger))),
            ],
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () => Future.wait([_load(), folders.load()]),
        child: ListView(padding: const EdgeInsets.fromLTRB(16, 4, 16, 24), children: [
          Container(
            padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
            decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(20)),
            child: Row(children: [
              FolderCover(folder: f, size: 64),
              const SizedBox(width: 16),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text(FolderLines(context).holds(f.files, f.bytes), style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
                  const SizedBox(height: 3),
                  Text(t.folderSince(formatShortDate(f.createdAt, now, locale)), style: TextStyle(fontSize: 13.5, color: c.text3)),
                ]),
              ),
            ]),
          ),
          const SizedBox(height: 12),
          SettingsGroup(children: [EncryptionRow(folder: f, ask: widget.encrypt)]),
          SectionLabel(t.folderWhoSees),
          if (people == null)
            const Padding(padding: EdgeInsets.all(24), child: Center(child: CircularProgressIndicator()))
          else
            SettingsGroup(children: [
              for (final u in people.users)
                SettingsRow(
                  leading: Avatar(name: u.name, id: u.id, size: 40),
                  title: u.name,
                  subtitle: u.isAdmin ? t.folderAdminSeesAll : t.roleMember,
                  trailing: sees(u.id,
                      admin: u.isAdmin, given: u.folders.contains(f.id), set: (on) => _services.admin.setFolderPerson(f.id, u.id, sees: on)),
                ),
              for (final i in people.invites.where((i) => i.userId == null))
                SettingsRow(
                  leading: Avatar(name: i.name, id: i.id, size: 40, pending: true),
                  title: i.name,
                  subtitle: t.inviteOpenUntil(daysAgo(i.expiresAt, now) == 0 ? formatTime(i.expiresAt, locale) : formatWhen(i.expiresAt, now, locale)),
                  trailing: sees(i.id,
                      admin: i.role == Role.admin, given: i.folders.contains(f.id), set: (on) => _services.admin.setFolderInvite(f.id, i.id, gets: on)),
                ),
            ]),
          SectionLabel(t.folderPinsInto),
          SettingsGroup(children: [
            for (final p in sending)
              SettingsRow(
                leading: SettingsRow.icon(context, AppIcons.key),
                title: p.code,
                subtitle: p.kind == PinKind.day && p.expiresAt != null
                    ? '${t.pinsDay} · ${t.folderPinEnds(formatWhen(p.expiresAt!, now, locale))}'
                    : '${t.pinsPermanent} · ${t.folderPinSince(formatShortDate(p.createdAt, now, locale))}',
                onTap: () => _pin(p),
              ),
            SettingsRow(
              leading: SettingsRow.icon(context, AppIcons.plus, accent: true),
              title: t.folderNewPin,
              accent: true,
              trailing: const SizedBox.shrink(),
              onTap: _newPin,
            ),
          ]),
        ]),
      ),
    );
  }
}

/// A folder made or renamed, and whether a new one is to be encrypted.
typedef FolderName = ({FolderInfo folder, bool encrypt});

/// A new folder's name, or a new name for [folder]; the folder as it is now, or null. A new one
/// can be encrypted, as admins set for new folders by default.
class FolderNameDialog extends StatefulWidget {
  const FolderNameDialog({super.key, this.folder});
  final FolderInfo? folder;

  @override
  State<FolderNameDialog> createState() => _FolderNameDialogState();
}

class _FolderNameDialogState extends State<FolderNameDialog> {
  late final _name = TextEditingController(text: widget.folder?.name ?? '');
  String? _error;
  bool _busy = false;
  bool _encrypt = false;

  @override
  void initState() {
    super.initState();
    if (widget.folder == null) {
      Services.read(context).keys.newFoldersEncrypted().then((on) {
        if (mounted) setState(() => _encrypt = on);
      }, onError: (Object _) {});
    }
  }

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final t = AppLocalizations.of(context);
    final services = Services.read(context);
    final name = _name.text.trim();
    if (name.isEmpty || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final f = widget.folder;
      final made = f == null ? await services.admin.createFolder(name) : await services.admin.renameFolder(f.id, name);
      await services.folders.load();
      if (mounted) Navigator.pop(context, (folder: made, encrypt: f == null && _encrypt));
    } on Exception catch (e) {
      if (mounted) setState(() => _error = folderProblem(t, e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final renaming = widget.folder != null;
    // A bucket has no directory to rename with the folder.
    final s3 = Services.read(context).admin.storageMode == Storage.s3;
    return AlertDialog(
      title: Text(renaming ? t.folderRenameTitle : t.foldersNew),
      content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        FieldLabel(t.folderName),
        TextField(
          controller: _name,
          autofocus: true,
          maxLength: 60,
          textCapitalization: TextCapitalization.sentences,
          decoration: InputDecoration(
            counterText: '',
            errorText: _error,
            errorMaxLines: 3,
            helperText: _error == null ? (renaming ? (s3 ? null : t.folderRenameHelp) : t.folderNewHelp) : null,
            helperMaxLines: 3,
          ),
          onChanged: (_) => setState(() => _error = null),
          onSubmitted: (_) => _save(),
        ),
        if (!renaming)
          CheckboxListTile(
            value: _encrypt,
            onChanged: (v) => setState(() => _encrypt = v ?? false),
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            title: Text(t.encryptionNewFolder, style: const TextStyle(fontSize: 15)),
          ),
      ]),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: Text(t.commonCancel)),
        TextButton(onPressed: _busy || _name.text.trim().isEmpty ? null : _save, child: Text(renaming ? t.folderRename : t.folderCreate)),
      ],
    );
  }
}
