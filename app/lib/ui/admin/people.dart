import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';
import 'invite_person_screen.dart';

/// Screen 17's people: everyone with an account, then the invites nobody has used yet.
class PeopleGroup extends StatelessWidget {
  const PeopleGroup({super.key, required this.people, required this.onChanged});
  final People people;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final now = clock.now();
    return SettingsGroup(children: [
      for (final p in people.users)
        SettingsRow(
          leading: Avatar(name: p.name, id: p.id, size: 40),
          title: p.name,
          subtitle: p.isMe ? t.personYou(p.phones.length) : _activity(t, p, now),
          trailing: RoleBadge(label: p.isAdmin ? t.roleAdmin : t.roleMember, admin: p.isAdmin, crown: false),
          onTap: () async {
            await showModalBottomSheet<void>(context: context, isScrollControlled: true, builder: (_) => PersonSheet(person: p));
            onChanged();
          },
        ),
      for (final i in people.invites)
        SettingsRow(
          leading: Avatar(name: i.name, id: i.id, size: 40, pending: true),
          title: i.name,
          subtitle: t.inviteOpenUntil(daysAgo(i.expiresAt, now) == 0 ? formatTime(i.expiresAt, locale) : formatWhen(i.expiresAt, now, locale)),
          trailing: Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
            decoration: BoxDecoration(color: c.warnSoft, borderRadius: BorderRadius.circular(8)),
            child: Text(t.badgeInvited, style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: c.warn)),
          ),
          onTap: () async {
            await showModalBottomSheet<void>(context: context, builder: (_) => _InviteSheet(invite: i, people: people));
            onChanged();
          },
        ),
    ]);
  }

  static String _activity(AppLocalizations t, Person p, DateTime now) {
    final phones = t.personPhoneCount(p.phones.length);
    final seen = p.lastSeenAt;
    if (seen == null) return phones;
    final days = daysAgo(seen, now);
    final active = switch (days) { <= 0 => t.activeToday, 1 => t.activeYesterday, _ => t.activeDaysAgo(days) };
    return '$phones · $active';
  }
}

/// One person: their phones, their role, and removing them.
class PersonSheet extends StatefulWidget {
  const PersonSheet({super.key, required this.person});
  final Person person;

  @override
  State<PersonSheet> createState() => _PersonSheetState();
}

class _PersonSheetState extends State<PersonSheet> {
  late Person _person = widget.person;
  bool _busy = false;

  /// Runs [change]; a refusal (the last admin) shows up as a message.
  Future<bool> _do(Future<void> Function() change) async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _busy = true);
    try {
      await change();
      return true;
    } on ApiException catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(e.code == 'last_admin' ? t.personLastAdmin : t.commonFailed)));
    } on NetworkException {
      messenger.showSnackBar(SnackBar(content: Text(t.commonOffline)));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    return false;
  }

  Future<bool> _confirm(String title, String body, String action) async {
    final t = AppLocalizations.of(context);
    return await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(title),
            content: Text(body, style: TextStyle(color: context.colors.text2, height: 1.5)),
            actions: [
              TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
              TextButton(
                style: TextButton.styleFrom(foregroundColor: context.colors.danger),
                onPressed: () => Navigator.pop(context, true),
                child: Text(action),
              ),
            ],
          ),
        ) ??
        false;
  }

  Future<void> _role(Role role) async {
    final admin = Services.read(context).admin;
    if (await _do(() => admin.setRole(_person.id, role)) && mounted) {
      setState(() => _person = _person.copyWith(role: role));
    }
  }

  Future<void> _signOut(Phone phone) async {
    final t = AppLocalizations.of(context);
    final admin = Services.read(context).admin;
    if (!await _confirm(t.personSignOutTitle(phone.name), t.personSignOutBody, t.personSignOutPhone)) return;
    if (await _do(() => admin.signOutPhone(phone.id)) && mounted) {
      setState(() => _person = _person.copyWith(phones: [for (final p in _person.phones) if (p.id != phone.id) p]));
    }
  }

  Future<void> _remove() async {
    final t = AppLocalizations.of(context);
    final admin = Services.read(context).admin;
    final navigator = Navigator.of(context);
    if (!await _confirm(t.personRemoveTitle(_person.name), t.personRemoveBody, t.personRemove(_person.name))) return;
    if (await _do(() => admin.removePerson(_person.id))) navigator.pop();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final now = clock.now();
    final p = _person;
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 12),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
          Row(children: [
            Avatar(name: p.name, id: p.id, size: 56),
            const SizedBox(width: 14),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(p.name, style: Theme.of(context).textTheme.headlineSmall),
                if (p.username != null) Text('@${p.username}', style: TextStyle(fontSize: 14, color: c.text3)),
              ]),
            ),
            RoleBadge(label: p.isAdmin ? t.roleAdmin : t.roleMember, admin: p.isAdmin),
          ]),
          SectionLabel(t.personPhones),
          SettingsGroup(children: [
            for (final phone in p.phones)
              SettingsRow(
                leading: SettingsRow.icon(context, AppIcons.smartphone),
                title: phone.name,
                subtitle: phone.isThis ? t.personThisPhone : t.personLastUsed(formatWhen(phone.lastSeenAt, now, locale)),
                trailing: phone.isThis
                    ? const SizedBox()
                    : TextButton(
                        style: TextButton.styleFrom(foregroundColor: c.danger),
                        onPressed: _busy ? null : () => _signOut(phone),
                        child: Text(t.personSignOutPhone),
                      ),
              ),
            SettingsRow(
              leading: SettingsRow.icon(context, AppIcons.plus, accent: true),
              title: t.personAddPhone,
              onTap: () => Navigator.push(context, MaterialPageRoute<void>(builder: (_) => InvitePersonScreen(forPerson: p))),
            ),
          ]),
          const SizedBox(height: 14),
          SettingsGroup(children: [
            SettingsRow(
              leading: SettingsRow.icon(context, AppIcons.crown),
              title: p.isAdmin ? t.personMakeMember : t.personMakeAdmin,
              subtitle: p.isAdmin ? t.roleMemberLong : t.roleAdminLong,
              trailing: const SizedBox(),
              onTap: _busy ? null : () => _role(p.isAdmin ? Role.member : Role.admin),
            ),
            if (!p.isMe)
              SettingsRow(
                leading: SettingsRow.icon(context, AppIcons.userX),
                title: t.personRemove(p.name),
                danger: true,
                trailing: const SizedBox(),
                onTap: _busy ? null : _remove,
              ),
          ]),
        ]),
      ),
    );
  }
}

class _InviteSheet extends StatelessWidget {
  const _InviteSheet({required this.invite, required this.people});
  final OpenInvite invite;
  final People people;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).languageCode;
    final owner = invite.userId == null ? null : people.users.where((u) => u.id == invite.userId).firstOrNull;
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 12),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
          Row(children: [
            Avatar(name: invite.name, id: invite.id, size: 56, pending: true),
            const SizedBox(width: 14),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(invite.name, style: Theme.of(context).textTheme.headlineSmall),
                Text(owner != null ? t.inviteForPhone(owner.name) : t.inviteOpenUntil(formatWhen(invite.expiresAt, clock.now(), locale)),
                    style: TextStyle(fontSize: 14, color: context.colors.text3)),
              ]),
            ),
          ]),
          const SizedBox(height: 18),
          SettingsGroup(children: [
            SettingsRow(
              leading: SettingsRow.icon(context, AppIcons.x),
              title: t.inviteWithdraw,
              danger: true,
              trailing: const SizedBox(),
              onTap: () async {
                final navigator = Navigator.of(context);
                try {
                  await Services.read(context).admin.withdrawInvite(invite.id);
                } on Exception {
                  // It shows up again in the list.
                }
                navigator.pop();
              },
            ),
          ]),
        ]),
      ),
    );
  }
}
