import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:flutter/foundation.dart'
    show
        TargetPlatform,
        debugDefaultTargetPlatformOverride,
        defaultTargetPlatform;
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/notifications/agent_notifications.dart';
import 'package:magic_cli_remote/data/notifications/notification_coordinator.dart';
import 'package:magic_cli_remote/data/notifications/notification_service.dart';
import 'package:magic_cli_remote/data/notifications/foreground_service.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/data/protocol/transport_policy.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

class _AskClient extends McremoteClient {
  _AskClient() : super(settings: SettingsStore());

  final eventController = StreamController<SessionEvent>.broadcast();
  final connectionController = StreamController<McConnectionState>.broadcast();
  McConnectionState current = McConnectionState.disconnected;
  List<SessionEvent> snapshot = const [];
  Object? snapshotError;

  @override
  Stream<SessionEvent> get events => eventController.stream;

  @override
  Stream<McConnectionState> get connectionStates => connectionController.stream;

  @override
  McConnectionState get state => current;

  final responded = <String>[];

  @override
  Future<void> respondPermission({
    required String sessionId,
    required String permissionId,
    String? optionId,
    bool cancelled = false,
  }) async {
    responded.add('$sessionId:$permissionId:${optionId ?? ''}:$cancelled');
  }

  @override
  Future<List<SessionEvent>> pendingAsks() async {
    final error = snapshotError;
    if (error != null) throw error;
    return snapshot;
  }

  void emitConnection(McConnectionState state) {
    current = state;
    connectionController.add(state);
  }

  Future<void> close() async {
    await eventController.close();
    await connectionController.close();
  }
}

class _Notifications extends NotificationService {
  final shown = <String>[];
  final cancelled = <String>[];
  final expired = <String>[];
  final errors = <String?>[];

  String _key(String kind, String sessionId, String requestId) =>
      '$kind:$sessionId:$requestId';

  @override
  Future<void> init() async {}

  @override
  Future<NotifResponse?> takeLaunchResponse() async => null;

  @override
  Future<void> showPermission({
    required String sessionId,
    required String permissionId,
    required String toolName,
    String? detail,
    String? allowOptionId,
  }) async => shown.add(_key('permission', sessionId, permissionId));

  @override
  Future<void> showQuestion({
    required String sessionId,
    required String questionId,
    required String sessionLabel,
    String? detail,
  }) async => shown.add(_key('question', sessionId, questionId));

  @override
  Future<void> showAskExpired({
    required NotifKind kind,
    required String sessionId,
    required String requestId,
    required String sessionLabel,
  }) async => expired.add(_key(kind.name, sessionId, requestId));

  @override
  Future<void> showError({
    required String sessionId,
    required String sessionLabel,
    String? detail,
  }) async => errors.add(detail);

  @override
  Future<void> cancelPermission(String sessionId, String permissionId) async =>
      cancelled.add(_key('permission', sessionId, permissionId));

  @override
  Future<void> cancelQuestion(String sessionId, String questionId) async =>
      cancelled.add(_key('question', sessionId, questionId));
}

/// Stands in for the real iOS implementation so the service's fall-through
/// plumbing (android == null → iOS) is exercised without method channels.
class _FakeIosPlugin extends IOSFlutterLocalNotificationsPlugin {
  bool permissionsRequested = false;

  @override
  Future<bool?> requestPermissions({
    bool sound = false,
    bool alert = false,
    bool badge = false,
    bool provisional = false,
    bool critical = false,
    bool carPlay = false,
    bool providesAppNotificationSettings = false,
  }) async {
    permissionsRequested = true;
    return true;
  }

  @override
  Future<NotificationsEnabledOptions?> checkPermissions() async =>
      const NotificationsEnabledOptions(
        isEnabled: true,
        isSoundEnabled: true,
        isAlertEnabled: true,
        isBadgeEnabled: true,
        isProvisionalEnabled: false,
        isCriticalEnabled: false,
        isProvidesAppNotificationSettingsEnabled: false,
      );
}

/// Captures what the service hands the plugin: initialization settings and
/// per-show details. Resolves only the fake iOS implementation, so Android
/// setup is skipped — the shape a real iOS device produces.
///
/// `implements` (not `extends`): the real plugin's only public constructor
/// is a singleton factory. Members the service never touches fall through to
/// the throwing [noSuchMethod] so silent no-ops can't hide a broken test.
class _CapturingPlugin implements FlutterLocalNotificationsPlugin {
  final ios = _FakeIosPlugin();
  InitializationSettings? initSettings;
  final shown = <NotificationDetails?>[];

  @override
  Future<void> cancel({required int id, String? tag}) async {}

  @override
  Future<NotificationAppLaunchDetails?>
  getNotificationAppLaunchDetails() async => null;

  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnimplementedError('$invocation');

  @override
  Future<bool?> initialize({
    required InitializationSettings settings,
    DidReceiveNotificationResponseCallback? onDidReceiveNotificationResponse,
    DidReceiveBackgroundNotificationResponseCallback?
    onDidReceiveBackgroundNotificationResponse,
  }) async {
    initSettings = settings;
    return true;
  }

  @override
  Future<void> show({
    required int id,
    String? title,
    String? body,
    NotificationDetails? notificationDetails,
    String? payload,
  }) async {
    shown.add(notificationDetails);
  }

  @override
  T? resolvePlatformSpecificImplementation<
    T extends FlutterLocalNotificationsPlatform
  >() {
    if (T == IOSFlutterLocalNotificationsPlugin) return ios as T;
    return null;
  }
}

/// A client wedged in the parked state (fast reconnect loop exhausted) that
/// records slow maintenance retries instead of dialling.
class _ParkedClient extends McremoteClient {
  _ParkedClient() : super(settings: SettingsStore());

  int reconnects = 0;

  @override
  bool get reconnectParked => true;

  @override
  bool get hasCredentials => true;

  @override
  Future<void> reconnect({
    String? hostInput,
    String? token,
    TransportMode? transport,
    bool allowTransportFallback = true,
    bool userInitiated = true,
  }) async {
    reconnects++;
  }
}

class _ForegroundService extends ForegroundServiceController {
  @override
  Future<void> start() async {}

  @override
  Future<void> stop() async {}

  @override
  Future<void> update({required String title, required String text}) async {}
}

Future<void> _settle() => Future<void>.delayed(Duration.zero);

void main() {
  group('NotifPayload codec', () {
    test('round-trips a permission payload with allow option', () {
      const p = NotifPayload(
        kind: NotifKind.permission,
        sessionId: 's1',
        permissionId: 'perm-9',
        allowOptionId: 'allow_once',
      );
      final back = NotifPayload.decode(p.encode());
      expect(back, p);
    });

    test('round-trips a turn-complete payload', () {
      const p = NotifPayload(kind: NotifKind.turnComplete, sessionId: 's2');
      expect(NotifPayload.decode(p.encode()), p);
    });

    test('rejects malformed / empty / unknown-kind input', () {
      expect(NotifPayload.decode(null), isNull);
      expect(NotifPayload.decode(''), isNull);
      expect(NotifPayload.decode('not json'), isNull);
      expect(NotifPayload.decode('{"k":"bogus","s":"x"}'), isNull);
      expect(NotifPayload.decode('{"k":"permission"}'), isNull); // no session
    });
  });

  group('shouldNotify', () {
    test('notifies for permission and turn_complete when not watching', () {
      expect(
        shouldNotify(eventType: 'permission_request', watching: false),
        isTrue,
      );
      expect(shouldNotify(eventType: 'turn_complete', watching: false), isTrue);
      expect(shouldNotify(eventType: 'error', watching: false), isTrue);
    });

    test('suppresses when the user is watching that session', () {
      expect(
        shouldNotify(eventType: 'permission_request', watching: true),
        isFalse,
      );
      expect(shouldNotify(eventType: 'turn_complete', watching: true), isFalse);
      expect(shouldNotify(eventType: 'error', watching: true), isFalse);
    });

    test('ignores unrelated event types', () {
      expect(
        shouldNotify(eventType: 'assistant_message_chunk', watching: false),
        isFalse,
      );
      expect(shouldNotify(eventType: 'tool_call', watching: false), isFalse);
    });

    test('each kind independently suppressible', () {
      expect(
        shouldNotify(
          eventType: 'permission_request',
          watching: false,
          kinds: const NotifyKinds(asks: false),
        ),
        isFalse,
      );
      expect(
        shouldNotify(
          eventType: 'turn_complete',
          watching: false,
          kinds: const NotifyKinds(turnComplete: false),
        ),
        isFalse,
      );
      expect(
        shouldNotify(
          eventType: 'error',
          watching: false,
          kinds: const NotifyKinds(errors: false),
        ),
        isFalse,
      );
      // Other kinds still fire.
      expect(
        shouldNotify(
          eventType: 'error',
          watching: false,
          kinds: const NotifyKinds(asks: false, turnComplete: false),
        ),
        isTrue,
      );
    });
  });

  group('notificationIdFor', () {
    test('is stable and positive, and distinguishes perm vs session', () {
      final permId = notificationIdFor(
        kind: NotifKind.permission,
        sessionId: 's1',
        requestId: 'p1',
      );
      final sessId = notificationIdFor(
        kind: NotifKind.turnComplete,
        sessionId: 's1',
      );
      expect(permId, greaterThanOrEqualTo(0));
      expect(sessId, greaterThanOrEqualTo(0));
      expect(permId, isNot(sessId));
      // Deterministic.
      expect(
        notificationIdFor(
          kind: NotifKind.permission,
          sessionId: 's1',
          requestId: 'p1',
        ),
        permId,
      );
    });
  });

  // MADR 0067 D4 (U3/U4) — Darwin wiring. Before this, init passed only
  // Android settings and every show() only Android details, so iOS presented
  // nothing, silently.
  group('Darwin wiring', () {
    test('init registers Darwin settings with a foreground Allow/Deny '
        'category and requests permission explicitly', () async {
      final plugin = _CapturingPlugin();
      final s = NotificationService(plugin);
      addTearDown(s.dispose);
      await s.init();

      final darwin = plugin.initSettings?.iOS;
      expect(darwin, isNotNull);
      // Permission must come from the explicit request below, not as an
      // initialize side effect — same placement as the Android request.
      expect(darwin!.requestAlertPermission, isFalse);
      expect(darwin.requestBadgePermission, isFalse);
      expect(darwin.requestSoundPermission, isFalse);
      expect(plugin.ios.permissionsRequested, isTrue);

      final cat = darwin.notificationCategories.single;
      expect(cat.identifier, 'approval_actions');
      expect(cat.actions.map((a) => a.identifier), ['allow', 'deny']);
      for (final a in cat.actions) {
        expect(
          a.options,
          contains(DarwinNotificationActionOption.foreground),
          reason:
              'a suspended iOS process has no WebSocket — actions must '
              'launch the app (${a.identifier})',
        );
      }
    });

    test('all four kinds carry iOS presentation details', () async {
      final plugin = _CapturingPlugin();
      final s = NotificationService(plugin);
      addTearDown(s.dispose);
      await s.showPermission(
        sessionId: 's',
        permissionId: 'p',
        toolName: 'bash',
      );
      await s.showQuestion(sessionId: 's', questionId: 'q', sessionLabel: 'l');
      await s.showTurnComplete(sessionId: 's', sessionLabel: 'l');
      await s.showError(sessionId: 's', sessionLabel: 'l');

      expect(plugin.shown, hasLength(4));
      for (final d in plugin.shown) {
        expect(d?.iOS, isNotNull);
      }
      // Only the permission kind carries the action category.
      expect(plugin.shown[0]?.iOS?.categoryIdentifier, 'approval_actions');
      expect(plugin.shown[1]?.iOS?.categoryIdentifier, isNull);
    });

    test(
      'permission plumbing falls through to the iOS implementation',
      () async {
        final plugin = _CapturingPlugin();
        final s = NotificationService(plugin);
        addTearDown(s.dispose);
        // Before 0067 P2 both resolved only the Android plugin, returned null
        // on iOS, and the Settings "blocked" warning could never appear there.
        expect(await s.areNotificationsEnabled(), isTrue);
        expect(await s.requestPermission(), isTrue);
      },
    );
  });

  // MADR 0067 D2/F3 (U2) — the background maintenance retry exists to feed
  // the Android keep-alive service. On iOS the process suspends and Timers
  // never fire; an armed one is dead at best and a stale reconnect burst on
  // resume at worst, so the arm site is platform-gated.
  group('background maintenance platform gate', () {
    NotificationCoordinator coordFor(_ParkedClient client) =>
        NotificationCoordinator(
          client: client,
          notifications: _Notifications(),
          service: _ForegroundService(),
        );

    test('Android: a parked client gets the slow background retry', () {
      // flutter_test pins defaultTargetPlatform to android — stated so the
      // symmetry with the iOS case below is visible.
      expect(defaultTargetPlatform, TargetPlatform.android);
      fakeAsync((fa) {
        final client = _ParkedClient();
        final c = coordFor(client)..enabled = true;
        c.setAppForegrounded(false);
        fa.elapse(const Duration(minutes: 5));
        expect(client.reconnects, 1);
      });
    });

    test('iOS: the retry timer is never armed', () {
      debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
      addTearDown(() => debugDefaultTargetPlatformOverride = null);
      fakeAsync((fa) {
        final client = _ParkedClient();
        final c = coordFor(client)..enabled = true;
        c.setAppForegrounded(false);
        // Well past the max backoff: nothing may ever fire.
        fa.elapse(const Duration(minutes: 90));
        expect(client.reconnects, 0);
      });
    });
  });

  group('NotificationCoordinator watch stack', () {
    NotificationCoordinator coord() => NotificationCoordinator(
      client: McremoteClient(settings: SettingsStore()),
    );

    test('claim/release tracks the topmost chat screen', () {
      final c = coord();
      expect(c.currentSessionId, isNull);
      c.claimSession('a');
      expect(c.currentSessionId, 'a');
      c.claimSession('b');
      expect(c.currentSessionId, 'b');
      // Chat B pops: A is visible again and must win the watching check —
      // the old single-field logic left this null and notified about A while
      // the user was looking at it.
      c.releaseSession('b');
      expect(c.currentSessionId, 'a');
      c.releaseSession('a');
      expect(c.currentSessionId, isNull);
    });

    test('re-claiming an id already on the stack moves it to the top', () {
      final c = coord();
      c.claimSession('a');
      c.claimSession('b');
      c.claimSession('a');
      expect(c.currentSessionId, 'a');
      c.releaseSession('a');
      expect(c.currentSessionId, 'b');
    });

    test('releasing an id not on the stack is a no-op', () {
      final c = coord();
      c.claimSession('a');
      c.releaseSession('zzz');
      expect(c.currentSessionId, 'a');
    });

    test('two screens for one session need two releases', () {
      final c = coord();
      // A double-tapped row, or a notification tap while already in that
      // chat, pushes the same session twice. De-duplicating the claim let the
      // first dispose drop the only entry while an identical chat was still
      // on screen — and the coordinator then notified about it (L-12).
      c.claimSession('a');
      c.claimSession('a');
      c.releaseSession('a');
      expect(
        c.currentSessionId,
        'a',
        reason: 'a chat for this session is still visible',
      );
      c.releaseSession('a');
      expect(c.currentSessionId, isNull);
    });
  });

  group('NotificationCoordinator pending asks', () {
    late _AskClient client;
    late _Notifications notifications;
    late NotificationCoordinator coordinator;

    setUp(() async {
      client = _AskClient();
      notifications = _Notifications();
      coordinator = NotificationCoordinator(
        client: client,
        notifications: notifications,
        service: _ForegroundService(),
      );
      await coordinator.start();
    });

    tearDown(() async {
      await coordinator.dispose();
      await client.close();
    });

    test('reconnect snapshot cancels stale asks and is idempotent', () async {
      client.eventController.add(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          permissionId: 'p1',
        ),
      );
      await _settle();
      expect(notifications.shown, ['permission:s1:p1']);

      client.snapshot = const [];
      client.emitConnection(McConnectionState.error);
      client.emitConnection(McConnectionState.connected);
      await _settle();
      await _settle();
      expect(notifications.cancelled, ['permission:s1:p1']);

      client.emitConnection(McConnectionState.reconnecting);
      client.emitConnection(McConnectionState.connected);
      await _settle();
      expect(notifications.cancelled, ['permission:s1:p1']);
    });

    test('ending a session pulls its actionable notifications', () async {
      client.eventController.add(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          permissionId: 'p1',
        ),
      );
      client.eventController.add(
        SessionEvent(
          type: 'question_request',
          sessionId: 's2',
          questionId: 'q1',
        ),
      );
      await _settle();
      expect(notifications.shown, ['permission:s1:p1', 'question:s2:q1']);

      // The daemon emits no resolution when a session closes, and on a stable
      // connection nothing re-reads the pending-ask snapshot — so an Allow
      // notification for an ended session would sit there forever, and
      // tapping it would drop the user into a dead chat (MADR 0046 M-4).
      coordinator.dropSessionAsks('s1');
      await _settle();
      expect(notifications.cancelled, ['permission:s1:p1']);

      // Only that session's asks go; the other is untouched.
      expect(notifications.cancelled, isNot(contains('question:s2:q1')));

      // And it stays gone: a later refresh must not resurrect it.
      coordinator.claimSession('s2');
      coordinator.releaseSession('s2');
      await _settle();
      expect(
        notifications.shown.where((s) => s == 'permission:s1:p1').length,
        1,
      );
    });

    test('signing out pulls every actionable notification', () async {
      client.eventController.add(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          permissionId: 'p1',
        ),
      );
      client.eventController.add(
        SessionEvent(
          type: 'question_request',
          sessionId: 's2',
          questionId: 'q1',
        ),
      );
      await _settle();

      // After sign-out the responses could not be delivered even if tapped.
      coordinator.dropAllAsks();
      await _settle();
      expect(
        notifications.cancelled,
        containsAll(<String>['permission:s1:p1', 'question:s2:q1']),
      );
    });

    test('failed snapshot retains the current actionable ask', () async {
      client.eventController.add(
        SessionEvent(
          type: 'question_request',
          sessionId: 's1',
          questionId: 'q1',
        ),
      );
      await _settle();
      client.snapshotError = StateError('offline');
      client.emitConnection(McConnectionState.error);
      client.emitConnection(McConnectionState.connected);
      await _settle();
      await _settle();
      expect(notifications.shown, ['question:s1:q1']);
      expect(notifications.cancelled, isEmpty);
    });

    test('watching a session suppresses but retains its pending ask', () async {
      coordinator.claimSession('s1');
      client.eventController.add(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          permissionId: 'p1',
        ),
      );
      await _settle();
      expect(notifications.shown, isEmpty);

      coordinator.setAppForegrounded(false);
      await _settle();
      expect(notifications.shown, ['permission:s1:p1']);
    });
  });
  group('test notifications (MADR 0101 D)', () {
    late _AskClient client;
    late _Notifications notifications;
    late NotificationCoordinator coordinator;
    final opened = <String>[];

    setUp(() async {
      client = _AskClient();
      notifications = _Notifications();
      coordinator = NotificationCoordinator(
        client: client,
        notifications: notifications,
        service: _ForegroundService(),
      );
      coordinator.onOpenSession = opened.add;
      opened.clear();
      await coordinator.start();
    });

    tearDown(() async {
      await coordinator.dispose();
      await client.close();
    });

    test('sendTestNotification drives the real show paths', () async {
      await coordinator.sendTestNotification(NotifKind.permission);
      expect(notifications.shown, [
        'permission:$kTestNotificationSessionId:test',
      ]);
      await coordinator.sendTestNotification(NotifKind.error);
      expect(notifications.errors, ['This is a test error alert.']);
    });

    test('taps on a test notification neither navigate nor respond', () async {
      for (final action in NotifAction.values) {
        await coordinator.testHandleResponse(
          NotifResponse(
            action: action,
            payload: NotifPayload(
              kind: NotifKind.permission,
              sessionId: kTestNotificationSessionId,
              permissionId: 'test',
              allowOptionId: 'once',
            ),
          ),
        );
      }
      expect(opened, isEmpty, reason: 'a test tap must not navigate');
      expect(
        client.responded,
        isEmpty,
        reason: 'a test tap must never reach the daemon',
      );
      // Each tap retires the notification instead.
      expect(
        notifications.cancelled,
        List.filled(3, 'permission:$kTestNotificationSessionId:test'),
      );
    });

    test('a real permission response still routes', () async {
      await coordinator.testHandleResponse(
        NotifResponse(
          action: NotifAction.allow,
          payload: NotifPayload(
            kind: NotifKind.permission,
            sessionId: 's1',
            permissionId: 'p1',
            allowOptionId: 'once',
          ),
        ),
      );
      expect(client.responded, ['s1:p1:once:false']);
    });
  });

  group('NotificationCoordinator expiry tombstones (MADR 0101 A/E)', () {
    late _AskClient client;
    late _Notifications notifications;
    late NotificationCoordinator coordinator;

    setUp(() async {
      client = _AskClient();
      notifications = _Notifications();
      coordinator = NotificationCoordinator(
        client: client,
        notifications: notifications,
        service: _ForegroundService(),
      );
      coordinator.appForegrounded = false; // asks show immediately
      await coordinator.start();
    });

    tearDown(() async {
      await coordinator.dispose();
      await client.close();
    });

    SessionEvent request(String id) => SessionEvent(
      type: 'permission_request',
      sessionId: 's1',
      permissionId: id,
    );

    SessionEvent resolved(String id, {bool timedOut = false}) => SessionEvent(
      type: 'permission_resolved',
      sessionId: 's1',
      permissionId: id,
      timedOut: timedOut,
    );

    test('an expired shown ask leaves a tombstone, not a cancel', () async {
      client.eventController.add(request('p1'));
      await _settle();
      expect(notifications.shown, ['permission:s1:p1']);

      client.eventController.add(resolved('p1', timedOut: true));
      await _settle();
      expect(notifications.expired, ['permission:s1:p1']);
      expect(
        notifications.cancelled,
        isEmpty,
        reason:
            'the tombstone replaces in place; a cancel would delete '
            'the only evidence the agent asked',
      );
    });

    test('a human-resolved ask cancels as before', () async {
      client.eventController.add(request('p1'));
      await _settle();

      client.eventController.add(resolved('p1'));
      await _settle();
      expect(notifications.cancelled, ['permission:s1:p1']);
      expect(notifications.expired, isEmpty);
    });

    test('expiry of an ask that never showed is silent', () async {
      // Watching s1 suppresses the notification; the user saw the sheet and
      // the transcript's timeout line, so no tombstone either.
      coordinator.appForegrounded = true;
      coordinator.claimSession('s1');
      client.eventController.add(request('p1'));
      await _settle();
      expect(notifications.shown, isEmpty);

      client.eventController.add(resolved('p1', timedOut: true));
      await _settle();
      expect(notifications.expired, isEmpty);
      expect(notifications.cancelled, isEmpty);
    });

    test('question expiry gets the same tombstone', () async {
      client.eventController.add(
        SessionEvent(
          type: 'question_request',
          sessionId: 's1',
          questionId: 'q1',
        ),
      );
      await _settle();
      expect(notifications.shown, ['question:s1:q1']);

      client.eventController.add(
        SessionEvent(
          type: 'question_resolved',
          sessionId: 's1',
          questionId: 'q1',
          timedOut: true,
        ),
      );
      await _settle();
      expect(notifications.expired, ['question:s1:q1']);
    });

    test('session close still cancels, never tombstones', () async {
      client.eventController.add(request('p1'));
      await _settle();

      coordinator.dropSessionAsks('s1');
      await _settle();
      expect(notifications.cancelled, ['permission:s1:p1']);
      expect(notifications.expired, isEmpty);
    });

    test('tombstone payload has no request id, so a tap opens the session', () {
      final payload = NotifPayload(kind: NotifKind.permission, sessionId: 's1');
      final decoded = NotifPayload.decode(payload.encode());
      expect(decoded, isNotNull);
      expect(decoded!.permissionId, isNull);
      expect(decoded.questionId, isNull);
    });

    test(
      'error notifications carry the error field, text as fallback',
      () async {
        client.eventController.add(
          SessionEvent(
            type: 'error',
            sessionId: 's1',
            error: 'quota exhausted — resets at 04:00',
            text: '',
          ),
        );
        await _settle();
        expect(notifications.errors, ['quota exhausted — resets at 04:00']);

        client.eventController.add(
          SessionEvent(type: 'error', sessionId: 's1', text: 'legacy text'),
        );
        await _settle();
        expect(notifications.errors.last, 'legacy text');
      },
    );
  });

  // MADR 0128 D3. dispose() closed the response controller but left
  // _ready == true, and init() opens with `if (_ready) return;` — so a start()
  // after a dispose() skipped re-initialisation entirely and the first show*
  // added to a closed controller. Unreachable while the coordinator is
  // app-lifetime, but NotificationCoordinator.dispose() nulls its
  // subscriptions precisely so a later start() re-subscribes: the two halves
  // of that pair disagreed about whether restart is supported.
  group('restart after dispose (0128 D3)', () {
    test(
      'init() after dispose() re-initialises and show* does not throw',
      () async {
        final plugin = _CapturingPlugin();
        final s = NotificationService(plugin);

        await s.init();
        expect(plugin.initSettings, isNotNull);

        s.dispose();

        // The reset is what makes the second init() do anything at all.
        plugin.initSettings = null;
        await s.init();
        expect(
          plugin.initSettings,
          isNotNull,
          reason: '0128 D3: dispose() must clear _ready so init() runs again',
        );

        // The old controller is closed and cannot be reopened; a live stream
        // must be available, and delivery must not throw.
        final seen = <NotifResponse>[];
        final sub = s.responses.listen(seen.add);
        addTearDown(sub.cancel);
        addTearDown(s.dispose);

        await s.showTurnComplete(sessionId: 'sess-1', sessionLabel: 'After');
        expect(plugin.shown, isNotEmpty);
      },
    );
  });
}
