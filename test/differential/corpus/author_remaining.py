#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Author readable neutral JSON scenarios with exclusive creation, never mocks."""
from author_lifecycle import a, plugin, state, output, emit, teardown, CORE

U = {'$undefined': True}
OWNERSHIP = 'https://github.com/deepseek-ai/deepseek-harness/blob/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/packages/extensions/tool-cordis/tests/cordis-lifecycle.spec.ts'


def when(path, value, *program):
    return a('when', base={'$config': path}, value=value, program=list(program))


if __name__ == '__main__':
    emit(1, 'Extend inherits metadata and services without mutating its ancestor', ['extend-inheritance'], [
        plugin('consumer', [a('read', key='svc', to='service'), a('output', to='inherited', value={'$meta': 'scope'}), a('output', to='shadow', value={'$meta': 'name'})], needs={'svc': None})
    ], [a('provide', key='svc', value='root-value'), a('mount', context='grandchild', fiber='c', plugin='consumer', config={}),
        a('settle', label='active'), a('output', context='parent', to='parent-name', value={'$meta': 'name'}), a('snapshot', label='metadata')] + teardown('c'),
        [output('metadata', 'inherited', 'outer'), output('metadata', 'shadow', 'inner'), output('metadata', 'parent-name', 'parent'), output('active', 'service', 'root-value')],
        [dict(id='parent', parent='root', meta={'scope': 'outer', 'name': 'parent'}), dict(id='grandchild', parent='parent', meta={'name': 'inner'})])
    emit(2, 'Fresh isolation realms hold separate provisions with the same key', ['separate-isolation-realms'], [
        plugin('provider', [a('provide', key='svc', value={'$config': 'value'})]),
        plugin('consumer', [a('read', key='svc', to='seen')], needs={'svc': None})
    ], [a('mount', context='realm-a', fiber='pa', plugin='provider', config={'value': 'A'}),
        a('mount', context='realm-b', fiber='pb', plugin='provider', config={'value': 'B'}), a('settle', label='providers'),
        a('read', context='realm-a', key='svc', mode='get', to='a'), a('read', context='realm-b', key='svc', mode='get', to='b'),
        a('read', key='svc', mode='get', to='root'), a('snapshot', label='separate')] + teardown('pb', 'pa'),
        [output('separate', 'a', 'A'), output('separate', 'b', 'B'), output('separate', 'root', U)],
        [dict(id='realm-a', parent='root', isolate={'key': 'svc'}), dict(id='realm-b', parent='root', isolate={'key': 'svc'})])
    emit(3, 'Equal isolation labels join while a distinct label stays separate', ['equal-isolation-labels-join'], [
        plugin('provider', [a('provide', key='svc', value='joined')]), plugin('consumer', [a('read', key='svc', to='joined')], needs={'svc': None})
    ], [a('mount', context='first', fiber='p', plugin='provider', config={}), a('mount', context='second', fiber='c', plugin='consumer', config={}),
        a('mount', context='other', fiber='isolated', plugin='consumer', config={}), a('settle', label='joined')] + teardown('isolated', 'c', 'p'),
        [state('joined', 1, 'ACTIVE'), state('joined', 2, 'PENDING'), output('joined', 'joined', 'joined')],
        [dict(id='first', parent='root', isolate={'key': 'svc', 'label': 'shared'}), dict(id='second', parent='root', isolate={'key': 'svc', 'label': 'shared'}), dict(id='other', parent='root', isolate={'key': 'svc', 'label': 'different'})])
    emit(4, 'Duplicate provision is refused without replacing the current owner', ['duplicate-provision-refusal'], [
        plugin('provider', [a('provide', key='svc', value={'$config': 'value'})])
    ], [a('mount', fiber='p', plugin='provider', config={'value': 'first'}), a('settle', label='first'),
        a('mount', fiber='duplicate', plugin='provider', config={'value': 'second'}), a('settle', label='refused'), a('read', key='svc', mode='get', to='retained'), a('snapshot', label='retained')] + teardown('duplicate', 'p'),
        [state('refused', 1, 'FAILED'), output('retained', 'retained', 'first')])
    emit(5, 'Plugin service access requires a declaration, unchecked Get is explicit', ['required-declaration'], [
        plugin('provider', [a('provide', key='svc', value='value')]),
        plugin('undeclared', [a('read', key='svc', to='error', catch=True), a('read', key='svc', mode='get', to='unchecked')]),
        plugin('declared', [a('read', key='svc', to='declared')], needs={'svc': None})
    ], [a('mount', fiber='p', plugin='provider', config={}), a('settle', label='provided'), a('mount', fiber='u', plugin='undeclared', config={}), a('mount', fiber='d', plugin='declared', config={}), a('settle', label='checked')] + teardown('d', 'u', 'p'),
        [output('checked', 'error', {'name': 'Error', 'message': 'cannot get property "svc" without inject'}), output('checked', 'unchecked', 'value'), output('checked', 'declared', 'value')])
    emit(6, 'Inherited committed injection and explicit required lookup are distinct', ['inherited-inject-in-realm'], [
        plugin('provider', [a('provide', key='svc', value='outer')]),
        plugin('parent', [a('child', fiber='child', plugin='child', config={}), a('context', mode='isolate', to='private', key='svc', label='child-private'), a('child', context='private', fiber='private-child', plugin='private-child', config={}), a('child', context='private', fiber='private-required', plugin='private-required', config={})], needs={'svc': None}),
        plugin('child', [a('read', key='svc', to='inherited')]),
        plugin('private-child', [a('read', key='svc', to='retained-inherited', catch=True)]),
        plugin('private-required', [a('read', key='svc', to='must-stay-pending')], needs={'svc': None})
    ], [a('mount', fiber='provider', plugin='provider', config={}), a('mount', fiber='parent', plugin='parent', config={}), a('settle', label='children')] + teardown('parent', 'provider'),
        [output('children', 'inherited', 'outer'), output('children', 'retained-inherited', 'outer'), state('children', 4, 'PENDING')])
    emit(7, 'NeedWith config is bound to a consumer service view', ['needwith-config'], [
        plugin('provider', [a('provide', key='svc', mode='service', value={})]),
        plugin('consumer', [a('read', key='svc', mode='config', base={'base': True}, to='view')], needs={'svc': {'limit': 7, 'mode': 'need'}})
    ], [a('mount', fiber='p', plugin='provider', config={}), a('mount', fiber='c', plugin='consumer', config={}), a('settle', label='needwith')] + teardown('c', 'p'),
        [output('needwith', 'view', {'base': True, 'limit': 7, 'mode': 'need'})])
    emit(8, 'Intercept chain applies ancestors before descendants and NeedWith', ['ancestor-first-intercept-chain'], [
        plugin('provider', [a('provide', key='svc', mode='service', value={})]),
        plugin('consumer', [a('read', key='svc', mode='config', base={'base': True, 'winner': 'base'}, head={'head': True}, to='merged')], needs={'svc': {'need': True, 'winner': 'need'}})
    ], [a('mount', fiber='p', plugin='provider', config={}), a('mount', context='inner', fiber='c', plugin='consumer', config={}), a('settle', label='intercept')] + teardown('c', 'p'),
        [output('intercept', 'merged', {'base': True, 'winner': 'need', 'outer': True, 'inner': True, 'need': True, 'head': True})],
        [dict(id='outer', parent='root', intercept={'key': 'svc', 'config': {'outer': True, 'winner': 'outer'}}), dict(id='inner', parent='outer', intercept={'key': 'svc', 'config': {'inner': True, 'winner': 'inner'}})])
    emit(18, 'Failure is sticky across dependency loss and replacement', ['sticky-failure'], [
        plugin('provider', [a('provide', key='svc', value='provided')]),
        plugin('consumer', [a('effect', label='owned', cleanup=[a('mark', label='unwound')]), a('fail', error='sticky-startup')], needs={'svc': None})
    ], [a('mount', fiber='p1', plugin='provider', config={}), a('mount', fiber='c', plugin='consumer', config={}), a('settle', label='failed'),
        a('dispose', fiber='p1'), a('mount', fiber='p2', plugin='provider', config={}), a('settle', label='still-failed')] + teardown('c', 'p2'),
        [state('failed', 1, 'FAILED'), state('still-failed', 1, 'FAILED')], attribution=[CORE])
    emit(19, 'Explicit Update clears sticky failure and permits recovery', ['explicit-update-recovery'], [
        plugin('recoverable', [when('fail', True, a('fail', error='recoverable-startup')), a('output', to='version', value={'$config': 'v'})])
    ], [a('mount', fiber='f', plugin='recoverable', config={'fail': True, 'v': 1}), a('settle', label='failed'),
        a('update', fiber='f', config={'fail': False, 'v': 2}), a('settle', label='recovered')] + teardown('f'),
        [state('failed', 0, 'FAILED'), state('recovered', 0, 'ACTIVE'), output('recovered', 'version', 2)], attribution=[CORE])
    emit(20, 'A superseded load error cannot fail the latest update target', ['superseded-load-error', 'partial-load-cleanup'], [
        plugin('superseded', [a('effect', label='owned', cleanup=[a('mark', label='owned-cleanup', value={'$config': 'v'})]), when('v', 1, a('gate', label='old-load'), a('fail', error='superseded activation')), a('output', to='latest', value={'$config': 'v'})])
    ], [a('mount', fiber='f', plugin='superseded', config={'v': 1, 'cleanup': 'old-cleanup'}), a('waitMark', label='old-load'),
        a('update', fiber='f', config={'v': 2, 'cleanup': 'new-cleanup'}, **{'await': False}, task='new-target'), a('release', label='old-load'), a('settle', label='latest')] + teardown('f'),
        [state('latest', 0, 'ACTIVE'), output('latest', 'latest', 2)], attribution=[CORE])
    emit(21, 'An isolated provider is retained through blocked consumer teardown', ['provider-retained-through-consumer-teardown'], [
        plugin('provider', [a('provide', key='svc', value={'alive': True})], [a('mark', label='provider-cleanup')]),
        plugin('consumer', [a('read', key='svc', to='initial')], [a('gate', label='consumer-unload'), a('read', key='svc', to='during-teardown'), a('mark', label='consumer-cleanup')], needs={'svc': None})
    ], [a('mount', context='realm', fiber='p', plugin='provider', config={}), a('mount', context='realm', fiber='c', plugin='consumer', config={}), a('settle', label='active'),
        a('dispose', fiber='p', **{'await': False}, task='provider-dispose'), a('waitMark', label='consumer-unload'), a('snapshot', label='retained'),
        a('release', label='consumer-unload'), a('settle', label='released')] + teardown('c'),
        [state('retained', 0, 'UNLOADING'), state('retained', 1, 'UNLOADING'), output('released', 'during-teardown', {'alive': True}), state('released', 0, 'DISPOSED')],
        [dict(id='realm', parent='root', isolate={'key': 'svc', 'label': 'shared'})], [CORE])
    emit(22, 'Parent disposal joins child and late effect cleanup', ['child-teardown', 'late-disposer-join'], [
        plugin('parent', [a('child', fiber='child', plugin='child', config={})], [a('mark', label='parent-cleanup')]),
        plugin('child', [a('effect', label='late-setup', program=[a('gate', label='effect-setup')], cleanup=[a('gate', label='effect-cleanup'), a('mark', label='effect-cleaned')], **{'await': False})])
    ], [a('mount', fiber='parent', plugin='parent', config={}), a('waitMark', label='effect-setup'),
        a('dispose', fiber='parent', **{'await': False}, task='parent-dispose'), a('snapshot', label='joining-setup'),
        a('release', label='effect-setup'), a('waitMark', label='effect-cleanup'), a('snapshot', label='joining-cleanup'),
        a('release', label='effect-cleanup'), a('settle', label='disposed')],
        [state('joining-cleanup', 0, 'UNLOADING'), state('joining-cleanup', 1, 'UNLOADING'), state('disposed', 0, 'DISPOSED'), state('disposed', 1, 'DISPOSED')], attribution=[OWNERSHIP])
    emit(23, 'Partial effect setup unwinds collected disposers without retaining ownership', ['partial-effect-unwind', 'single-shot-effect'], [
        plugin('partial', [a('effect', label='throwing-setup', mode='partial', cleanup=[a('mark', label='first'), a('mark', label='second')], error='setup failed', catch=True, to='setup-error'),
                           a('effect', label='single-shot', cleanup=[a('mark', label='once')], to='single'), a('withdraw', task='single'), a('withdraw', task='single')])
    ], [a('mount', fiber='f', plugin='partial', config={}), a('settle', label='active'), a('inspect', fiber='f', mode='effects', to='effects'), a('snapshot', label='unwound')] + teardown('f'),
        [state('unwound', 0, 'ACTIVE'), output('unwound', 'effects', []), output('unwound', 'setup-error', {'name': 'Error', 'message': 'setup failed'})], attribution=[OWNERSHIP])
    emit(24, 'Effects are refused while unloading and after disposal', ['inactive-effect-refusal'], [
        plugin('effect-owner', [a('effect', label='owned', cleanup=[a('lateEffect', fiber='f', label='during-unload', catch=True, to='unload-error')])])
    ], [a('mount', fiber='f', plugin='effect-owner', config={}), a('settle', label='active'), a('dispose', fiber='f'),
        a('lateEffect', fiber='f', label='after-dispose', catch=True, to='disposed-error'), a('settle', label='refused')],
        [output('refused', 'unload-error', {'name': 'Error', 'message': 'cannot create effect on inactive context', 'code': 'INACTIVE_EFFECT'}), output('refused', 'disposed-error', {'name': 'Error', 'message': 'cannot create effect on inactive context', 'code': 'INACTIVE_EFFECT'})], attribution=[OWNERSHIP])
    emit(27, 'Raw config and transformed effective config are distinct retained values', ['raw-effective-config', 'internal-config-hooks'], [
        plugin('configured', [a('output', to='applied', value={'$config': ''})], configTransform={'default': 9})
    ], [a('mount', fiber='f', plugin='configured', config={'value': 1}), a('settle', label='initial'), a('rawConfig', fiber='f', config={'value': 2}), a('snapshot', label='raw-retained')] + teardown('f'),
        [dict(at='initial', path='/states/0/rawConfig', equals={'value': 1}), dict(at='initial', path='/states/0/effectiveConfig', equals={'value': 1, 'default': 9}), dict(at='raw-retained', path='/states/0/rawConfig', equals={'value': 2}), dict(at='raw-retained', path='/states/0/effectiveConfig', equals={'value': 1, 'default': 9})], attribution=[CORE])
    emit(28, 'Candidate config resolution does not publish; retained update commits raw input', ['resolve-config-no-publication', 'internal-update-retain', 'internal-commit-nosave'], [
        plugin('retained', [], update='retain', configTransform={'normalized': True})
    ], [a('mount', fiber='f', plugin='retained', config={'v': 0}), a('settle', label='initial'),
        a('resolveConfig', fiber='f', config={'v': 99}, to='candidate'), a('snapshot', label='resolved-only'), a('update', fiber='f', config={'v': 1}), a('settle', label='committed'),
        a('update', fiber='f', config={'v': 2}, mode='noSave'), a('settle', label='no-save')] + teardown('f'),
        [dict(at='resolved-only', path='/states/0/rawConfig', equals={'v': 0}), dict(at='resolved-only', path='/states/0/effectiveConfig', equals={'v': 0, 'normalized': True}), dict(at='committed', path='/states/0/revision', equals=0), dict(at='no-save', path='/states/0/effectiveConfig', equals={'v': 2, 'normalized': True})], attribution=[CORE])
    emit(29, 'Stale or rejected retained updates cannot publish effective config', ['stale-update-not-published', 'rejected-update-not-published'], [
        plugin('retained', [a('listen', key='internal/update', label='update-owner', program=[when('v', 1, a('gate', label='old-update')), when('v', 3, a('gate', label='reject-update'), a('fail', error='update rejected'))])])
    ], [a('mount', fiber='f', plugin='retained', config={'v': 0}), a('settle', label='initial'),
        a('update', fiber='f', config={'v': 1}, **{'await': False}, task='old-update'), a('waitMark', label='old-update'), a('snapshot', label='pending-update'),
        a('update', fiber='f', config={'v': 2}), a('release', label='old-update'), a('awaitTask', task='old-update'), a('settle', label='current'),
        a('update', fiber='f', config={'v': 3}, **{'await': False}, task='reject-update'), a('waitMark', label='reject-update'), a('release', label='reject-update'),
        a('awaitTask', task='reject-update', catch=True, to='rejection'), a('settle', label='rejected')] + teardown('f'),
        [dict(at='pending-update', path='/states/0/effectiveConfig', equals={'v': 0}), dict(at='current', path='/states/0/effectiveConfig', equals={'v': 2}), dict(at='rejected', path='/states/0/rawConfig', equals={'v': 3}), dict(at='rejected', path='/states/0/effectiveConfig', equals={'v': 2}), output('rejected', 'rejection', {'name': 'Error', 'message': 'update rejected'})], attribution=[CORE])
    emit(30, 'Unchanged target hook vetoes restart but changed target delegates once', ['unchanged-target-no-restart', 'delegated-effective-identity'], [
        plugin('unchanged-aware', [], update='unchanged', configTransform={'resolved': True})
    ], [a('mount', fiber='f', plugin='unchanged-aware', config={'v': 1}), a('settle', label='initial'), a('update', fiber='f', config={'v': 1}), a('settle', label='unchanged'),
        a('update', fiber='f', config={'v': 2}), a('settle', label='changed'), a('inspect', fiber='f', mode='configIdentity', to='identity'), a('snapshot', label='identity')] + teardown('f'),
        [dict(at='unchanged', path='/states/0/revision', equals=0), dict(at='changed', path='/states/0/revision', equals=1), output('identity', 'identity', True)], attribution=[CORE])
    emit(31, 'Emit is synchronous, does not await listeners, and supports symbol event keys', ['emit-mode', 'symbol-event-keys'], [], [
        a('listen', key='symbol-event', symbol=True, label='async-first', program=[a('gate', label='emit-listener'), a('mark', label='emit-finished')]),
        a('listen', key='symbol-event', symbol=True, label='sync-second', program=[a('mark', label='second-called')]),
        a('dispatch', key='symbol-event', symbol=True, mode='emit', to='emit-result'), a('snapshot', label='returned'), a('release', label='emit-listener'), a('waitMark', label='emit-finished'), a('settle', label='finished')],
        [output('returned', 'emit-result', U)], attribution=[CORE])
    emit(32, 'Parallel waits for every listener before aggregating errors', ['parallel-mode', 'parallel-all-settled'], [], [
        a('listen', key='parallel', label='fails', program=[a('fail', error='parallel failure')]),
        a('listen', key='parallel', label='slow', program=[a('gate', label='parallel-slow'), a('mark', label='parallel-complete')]),
        a('dispatch', key='parallel', mode='parallel', **{'await': False}, task='parallel'), a('waitMark', label='parallel-slow'), a('snapshot', label='in-flight'), a('release', label='parallel-slow'),
        a('awaitTask', task='parallel', catch=True, to='aggregate'), a('settle', label='finished')],
        [output('finished', 'aggregate', {'name': 'AggregateError', 'message': '', 'errors': [{'name': 'Error', 'message': 'parallel failure'}]})])
    emit(33, 'Serial awaits listeners then stops at zero bail value or first error', ['serial-mode', 'serial-stops-bail-and-error'], [], [
        a('listen', key='serial-bail', label='first', program=[a('gate', label='serial-gate'), a('return', value=False)]),
        a('listen', key='serial-bail', label='zero', program=[a('return', value=0)]), a('listen', key='serial-bail', label='not-called', program=[a('mark', label='wrong-after-bail')]),
        a('dispatch', key='serial-bail', mode='serial', to='zero-result', **{'await': False}, task='serial'), a('waitMark', label='serial-gate'), a('snapshot', label='serial-waiting'), a('release', label='serial-gate'), a('awaitTask', task='serial'),
        a('listen', key='serial-error', label='throws', program=[a('fail', error='serial failure')]), a('listen', key='serial-error', label='not-after-error', program=[a('mark', label='wrong-after-error')]),
        a('dispatch', key='serial-error', mode='serial', catch=True, to='serial-error'), a('settle', label='finished')],
        [output('finished', 'zero-result', 0), output('finished', 'serial-error', {'name': 'Error', 'message': 'serial failure'})])
    emit(34, 'Bail treats zero and empty string as values, not JavaScript truthiness', ['bail-mode', 'zero-empty-bail-values'], [], [
        a('listen', key='bail-zero', label='false', program=[a('return', value=False)]), a('listen', key='bail-zero', label='zero', program=[a('return', value=0)]),
        a('listen', key='bail-zero', label='not-after-zero', program=[a('mark', label='wrong-after-zero')]), a('dispatch', key='bail-zero', mode='bail', to='zero'),
        a('listen', key='bail-empty', label='null', program=[a('return', value=None)]), a('listen', key='bail-empty', label='empty', program=[a('return', value='')]),
        a('listen', key='bail-empty', label='not-after-empty', program=[a('mark', label='wrong-after-empty')]), a('dispatch', key='bail-empty', mode='bail', to='empty'), a('settle', label='finished')],
        [output('finished', 'zero', 0), output('finished', 'empty', '')])
    emit(35, 'Waterfall listener may wrap next or veto inner behavior', ['waterfall-veto', 'waterfall-wrap-order'], [], [
        a('listen', key='waterfall-veto', label='outer', program=[a('mark', label='outer-before'), a('next'), a('mark', label='outer-after')]),
        a('listen', key='waterfall-veto', label='veto', program=[a('return', value='vetoed')]), a('listen', key='waterfall-veto', label='not-after-veto', program=[a('next')]),
        a('dispatch', key='waterfall-veto', mode='waterfall', to='veto-result', value='inner-value'),
        a('listen', key='waterfall-value', label='delegates', program=[a('next', to='inner-result')]), a('dispatch', key='waterfall-value', mode='waterfall', to='delegated', value='inner-value'), a('settle', label='finished')],
        [output('finished', 'inner-result', 'inner-value'), output('finished', 'delegated', 'inner-value')])
    emit(36, 'Repeated next in a Waterfall listener throws exactly once', ['waterfall-repeated-next-error'], [], [
        a('listen', key='repeated', symbol=True, label='double-next', program=[a('next', to='first-next'), a('next')]),
        a('dispatch', key='repeated', symbol=True, mode='waterfall', value=7, catch=True, to='repeated-error'), a('settle', label='finished')],
        [output('finished', 'first-next', 7), output('finished', 'repeated-error', {'name': 'Error', 'message': 'next() called multiple times'})], attribution=[CORE])
