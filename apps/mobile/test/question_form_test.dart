import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/features/chat/question_sheet.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';
import 'package:magic_cli_remote/theme/celestial.dart';

// PLAN 0191 L3: the question sheet draws typed, optional and defaulted fields,
// checks them before Submit, offers Decline when the form allows it, and
// reopens with the daemon's message when an answer is refused (MADR 0191 D5).

Future<void> _pumpSheet(
  WidgetTester tester, {
  required List<QuestionItem> items,
  bool allowDecline = false,
  String? errorText,
  Map<String, List<String>>? initialAnswers,
  ValueChanged<Map<String, List<String>>>? onSubmit,
  VoidCallback? onDecline,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      theme: celestialLight,
      home: Scaffold(
        body: QuestionSheet(
          title: 'Release details',
          items: items,
          allowDecline: allowDecline,
          errorText: errorText,
          initialAnswers: initialAnswers,
          onSubmit: onSubmit ?? (_) {},
          onCancel: () {},
          onDecline: onDecline,
        ),
      ),
    ),
  );
}

VoidCallback? _submit(WidgetTester tester) => tester
    .widget<FilledButton>(find.byKey(const Key('question-submit')))
    .onPressed;

Future<void> _tapSubmit(WidgetTester tester) async {
  await tester.ensureVisible(find.byKey(const Key('question-submit')));
  await tester.tap(find.byKey(const Key('question-submit')));
  await tester.pump();
}

void main() {
  test('the typed-form fields and allow_decline are parsed', () {
    final item = QuestionItem.fromJson({
      'id': 'age',
      'kind': 'integer',
      'optional': true,
      'default': ['3'],
      'min': 0,
      'max': 130,
      'min_length': 1,
      'max_length': 8,
      'min_items': 1,
      'max_items': 2,
      'format': 'email',
      'pattern': '^a',
    });
    expect(item.kind, 'integer');
    expect(item.optional, isTrue);
    expect(item.defaults, ['3']);
    expect(item.min, 0);
    expect(item.max, 130);
    expect(item.minLength, 1);
    expect(item.maxLength, 8);
    expect(item.minItems, 1);
    expect(item.maxItems, 2);
    expect(item.format, 'email');
    expect(item.pattern, '^a');

    final ev = SessionEvent.fromJson({
      'type': 'question_request',
      'session_id': 's1',
      'question_id': 'q1',
      'allow_decline': true,
    });
    expect(ev.allowDecline, isTrue);
    expect(QuestionItem.fromJson({'id': 'x'}).optional, isFalse);
  });

  testWidgets('an optional field may be left empty', (tester) async {
    final submissions = <Map<String, List<String>>>[];
    await _pumpSheet(
      tester,
      items: [
        QuestionItem(id: 'name', header: 'Name'),
        QuestionItem(id: 'email', header: 'Email', optional: true),
      ],
      onSubmit: submissions.add,
    );
    expect(find.textContaining('optional'), findsOneWidget);
    await tester.enterText(find.byKey(const Key('question-text-0')), 'Ada');
    await tester.pump();
    expect(_submit(tester), isNotNull);
    await _tapSubmit(tester);
    expect(submissions.single['name'], ['Ada']);
    expect(submissions.single['email'], isEmpty);
  });

  testWidgets('a number field has a number keyboard and its bounds', (
    tester,
  ) async {
    await _pumpSheet(
      tester,
      items: [QuestionItem(id: 'age', kind: 'integer', min: 0, max: 10)],
    );
    final field = tester.widget<TextField>(
      find.byKey(const Key('question-text-0')),
    );
    expect(field.keyboardType.index, TextInputType.number.index);
    expect(find.textContaining('0 to 10'), findsOneWidget);

    await tester.enterText(find.byKey(const Key('question-text-0')), '11');
    await tester.pump();
    expect(find.textContaining('at most 10'), findsOneWidget);
    expect(_submit(tester), isNull);

    await tester.enterText(find.byKey(const Key('question-text-0')), '4.5');
    await tester.pump();
    expect(find.textContaining('whole number'), findsOneWidget);
    expect(_submit(tester), isNull);

    await tester.enterText(find.byKey(const Key('question-text-0')), '7');
    await tester.pump();
    expect(_submit(tester), isNotNull);
  });

  testWidgets('defaults are prefilled', (tester) async {
    final submissions = <Map<String, List<String>>>[];
    await _pumpSheet(
      tester,
      items: [
        QuestionItem(id: 'name', defaults: ['Ada']),
        QuestionItem(
          id: 'size',
          defaults: ['l'],
          options: [
            PermissionOption(optionId: 's', name: 'Small'),
            PermissionOption(optionId: 'l', name: 'Large'),
          ],
        ),
      ],
      onSubmit: submissions.add,
    );
    expect(find.text('Ada'), findsOneWidget);
    expect(_submit(tester), isNotNull, reason: 'defaults answer both fields');
    await _tapSubmit(tester);
    expect(submissions.single, {
      'name': ['Ada'],
      'size': ['l'],
    });
  });

  testWidgets('length, format and selection bounds are checked', (
    tester,
  ) async {
    await _pumpSheet(
      tester,
      items: [
        QuestionItem(id: 'email', format: 'email'),
        QuestionItem(id: 'code', maxLength: 3),
        QuestionItem(
          id: 'tags',
          multiple: true,
          maxItems: 1,
          options: [
            PermissionOption(optionId: 'a', name: 'A'),
            PermissionOption(optionId: 'b', name: 'B'),
          ],
        ),
      ],
    );
    await tester.enterText(find.byKey(const Key('question-text-0')), 'nope');
    await tester.enterText(find.byKey(const Key('question-text-1')), 'ABCD');
    await tester.tap(find.byKey(const Key('question-option-2-a')));
    await tester.tap(find.byKey(const Key('question-option-2-b')));
    await tester.pump();
    expect(find.textContaining('valid email'), findsOneWidget);
    expect(find.textContaining('at most 3 characters'), findsOneWidget);
    expect(find.textContaining('at most 1 choice'), findsOneWidget);
    expect(_submit(tester), isNull);
  });

  testWidgets('Decline appears only when the form allows it', (tester) async {
    var declines = 0;
    await _pumpSheet(
      tester,
      items: [QuestionItem(id: 'name')],
      allowDecline: true,
      onDecline: () => declines++,
    );
    await tester.ensureVisible(find.byKey(const Key('question-decline')));
    await tester.tap(find.byKey(const Key('question-decline')));
    await tester.tap(find.byKey(const Key('question-decline')));
    await tester.pump();
    expect(declines, 1);

    await _pumpSheet(tester, items: [QuestionItem(id: 'name')]);
    expect(find.byKey(const Key('question-decline')), findsNothing);
  });

  testWidgets('a refusal is shown with the answers kept', (tester) async {
    await _pumpSheet(
      tester,
      items: [QuestionItem(id: 'name', header: 'Name')],
      errorText: 'Name (name) must be at most 12 characters',
      initialAnswers: {
        'name': ['Grace Hopper Jr'],
      },
    );
    expect(
      find.textContaining('must be at most 12 characters'),
      findsOneWidget,
    );
    expect(find.text('Grace Hopper Jr'), findsOneWidget);
  });

  group('chat screen', () {
    testWidgets('a refused answer reopens the form with the message', (
      tester,
    ) async {
      final client = _FormClient(refuseFirst: true);
      await _pumpChat(tester, client);
      await tester.enterText(find.byKey(const Key('question-text-0')), 'Grace');
      await tester.pump();
      await _tapSubmit(tester);
      await tester.pumpAndSettle();

      expect(client.calls, hasLength(1));
      expect(find.byType(QuestionSheet), findsOneWidget);
      expect(
        find.textContaining('must be at most 3 characters'),
        findsOneWidget,
      );
      expect(find.text('Grace'), findsOneWidget, reason: 'the answer is kept');

      await tester.enterText(find.byKey(const Key('question-text-0')), 'Ada');
      await tester.pump();
      await _tapSubmit(tester);
      await tester.pumpAndSettle();
      expect(client.calls, hasLength(2));
      expect(client.calls.last, 'answers');
      expect(find.byType(QuestionSheet), findsNothing);
    });

    testWidgets('Decline sends declined', (tester) async {
      final client = _FormClient();
      await _pumpChat(tester, client);
      await tester.ensureVisible(find.byKey(const Key('question-decline')));
      await tester.tap(find.byKey(const Key('question-decline')));
      await tester.pumpAndSettle();
      expect(client.calls, ['declined']);
    });
  });
}

class _FormClient extends McremoteClient {
  _FormClient({this.refuseFirst = false});

  final bool refuseFirst;
  final calls = <String>[];

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'grok')],
        complete: true,
      );

  @override
  Future<void> respondQuestion({
    required String sessionId,
    required String questionId,
    Map<String, List<String>>? answers,
    bool cancelled = false,
    bool declined = false,
  }) async {
    calls.add(
      declined
          ? 'declined'
          : cancelled
          ? 'cancelled'
          : 'answers',
    );
    if (refuseFirst && calls.length == 1) {
      throw McException(
        'answer does not satisfy the form: Name (name) must be at most 3 characters',
        code: 'question_invalid',
      );
    }
  }
}

Future<void> _pumpChat(WidgetTester tester, _FormClient client) async {
  final question = SessionEvent.fromJson({
    'type': 'question_request',
    'session_id': 's1',
    'question_id': 'q1',
    'status': 'pending',
    'text': 'Release details',
    'allow_decline': true,
    'questions': [
      {'id': 'name', 'header': 'Name', 'max_length': 30},
    ],
  });
  final transcript = SessionTranscript(
    sessionId: 's1',
    items: [ChatItem.assistant('hi')],
    pendingQuestions: {'q1': question},
  );
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        mcremoteClientProvider.overrideWithValue(client),
        connectionStateProvider.overrideWith(
          (ref) => Stream.value(McConnectionState.connected),
        ),
        sessionTranscriptProvider('s1').overrideWithValue(transcript),
      ],
      child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
    ),
  );
  await tester.pumpAndSettle();
  expect(find.byType(QuestionSheet), findsOneWidget);
}
