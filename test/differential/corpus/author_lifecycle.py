#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Exclusive-write authoring helper for neutral D09-D17 JSON; not replay input."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def a(op, **fields):
    return dict(op=op, **fields)


def plugin(id, apply=(), cleanup=(), **fields):
    return dict(id=id, apply=list(apply), **(dict(cleanup=list(cleanup)) if cleanup else {}), **fields)


def state(at, index, value):
    return dict(at=at, path=f'/states/{index}/state', equals=value)


def output(at, key, value):
    return dict(at=at, path=f'/outputs/{key}', equals=value)


def emit(number, title, covers, plugins, steps, expect=(), contexts=(), attribution=()):
    script = dict(schema='s0-script/v1', id=f'D{number:02d}', title=title, covers=covers,
                  **(dict(contexts=contexts) if contexts else {}), plugins=plugins,
                  steps=steps, expect=list(expect), **(dict(attribution=attribution) if attribution else {}))
    with (ROOT / f'D{number:02d}.json').open('x') as file:
        json.dump(script, file, indent=2)
        file.write('\n')


def teardown(*fibers):
    return [a('dispose', fiber=f) for f in fibers] + [a('settle', label='disposed')]


CORE = 'https://github.com/deepseek-ai/deepseek-harness/blob/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/scripts/cordis-core-lifecycle.spec.ts'

if __name__ == '__main__':
    emit(9, 'Availability is evaluated using each consumer context', ['availability-consumer-binding'], [
        plugin('provider', [a('provide', key='svc', value={'ready': 'availability'}, mode='available')]),
        plugin('consumer', [a('mark', label='consumer-ready')], needs={'svc': None})
    ], [a('output', to='availability', value={'A': True, 'B': False}),
        a('mount', fiber='p', plugin='provider', config={}), a('settle', label='provider'),
        a('mount', context='a', fiber='ca', plugin='consumer', config={}),
        a('mount', context='b', fiber='cb', plugin='consumer', config={}), a('settle', label='split'),
        a('output', to='availability', value={'A': True, 'B': True}), a('notify', keys=['svc']), a('settle', label='joined')] + teardown('cb', 'ca', 'p'),
        [state('split', 1, 'ACTIVE'), state('split', 2, 'PENDING'), state('joined', 2, 'ACTIVE')],
        [dict(id='a', parent='root', meta={'audience': 'A'}), dict(id='b', parent='root', meta={'audience': 'B'})])
    emit(10, 'Awaiting a missing dependency settles PENDING rather than ACTIVE', ['pending-until-ready', 'await-not-active'], [
        plugin('consumer', [a('read', key='svc', to='seen')], needs={'svc': None})
    ], [a('mount', fiber='c', plugin='consumer', config={}), a('awaitFiber', fiber='c'), a('settle', label='pending')] + teardown('c'), [state('pending', 0, 'PENDING')])
    emit(11, 'A provision from a LOADING provider is not strictly visible', ['active-only-provider', 'loading-provider'], [
        plugin('provider', [a('provide', key='svc', value='ready'), a('gate', label='load')]),
        plugin('consumer', [a('read', key='svc', to='consumer')], needs={'svc': None})
    ], [a('mount', fiber='p', plugin='provider', config={}), a('waitMark', label='load'),
        a('mount', fiber='c', plugin='consumer', config={}), a('read', key='svc', mode='get', to='strict'),
        a('read', key='svc', mode='get', strict=False, to='retained'), a('snapshot', label='loading'),
        a('release', label='load'), a('settle', label='active')] + teardown('c', 'p'),
        [state('loading', 0, 'LOADING'), state('loading', 1, 'PENDING'), output('loading', 'strict', {'$undefined': True}), output('loading', 'retained', 'ready'), state('active', 1, 'ACTIVE')])
    emit(12, 'A late provider wakes an already pending consumer', ['late-provider-activation'], [
        plugin('consumer', [a('read', key='svc', to='consumer')], needs={'svc': None}),
        plugin('provider', [a('provide', key='svc', value='late')])
    ], [a('mount', fiber='c', plugin='consumer', config={}), a('settle', label='waiting'),
        a('mount', fiber='p', plugin='provider', config={}), a('settle', label='ready')] + teardown('c', 'p'),
        [state('waiting', 0, 'PENDING'), state('ready', 0, 'ACTIVE'), output('ready', 'consumer', 'late')])
    emit(13, 'All six lifecycle states are observed behind causal barriers', ['six-states'], [
        plugin('pending', [], needs={'absent': None}), plugin('loading', [a('gate', label='load')]),
        plugin('active', [a('mark', label='active-apply')]), plugin('failed', [a('fail', error='deliberate-failure')]),
        plugin('unloading', [], [a('gate', label='unload')]), plugin('disposed', [])
    ], [a('mount', fiber='p', plugin='pending', config={}), a('mount', fiber='l', plugin='loading', config={}), a('waitMark', label='load'),
        a('mount', fiber='a', plugin='active', config={}), a('mount', fiber='f', plugin='failed', config={}),
        a('mount', fiber='u', plugin='unloading', config={}), a('mount', fiber='d', plugin='disposed', config={}),
        a('waitMark', label='active-apply'), a('dispose', fiber='d'), a('dispose', fiber='u', **{'await': False}), a('waitMark', label='unload'),
        a('snapshot', label='six'), a('release', label='load'), a('release', label='unload'), a('settle', label='finished')] + teardown('a', 'f', 'l', 'p'),
        [state('six', i, s) for i, s in enumerate(['PENDING', 'LOADING', 'ACTIVE', 'FAILED', 'UNLOADING', 'DISPOSED'])])
    emit(14, 'Revision changes independently from provision identity epochs', ['revision-epochs', 'provision-uid-epochs'], [
        plugin('provider', [a('provide', key='svc', value={'$config': ''})]),
        plugin('consumer', [a('read', key='svc', to='seen')], needs={'svc': None})
    ], [a('mount', fiber='p', plugin='provider', config={'v': 1}), a('mount', fiber='c', plugin='consumer', config={}),
        a('settle', label='initial'), a('update', fiber='p', config={'v': 2}), a('settle', label='updated')] + teardown('c', 'p'),
        [output('initial', 'seen', {'v': 1}), output('updated', 'seen', {'v': 2}), dict(at='updated', path='/states/0/revision', equals=1)])
    emit(15, 'Disposing and replacing a provider changes the consumer binding', ['provider-swap'], [
        plugin('provider', [a('provide', key='svc', value={'$config': 'v'})]),
        plugin('consumer', [a('read', key='svc', to='seen')], needs={'svc': None})
    ], [a('mount', fiber='p1', plugin='provider', config={'v': 'old'}), a('mount', fiber='c', plugin='consumer', config={}), a('settle', label='old'),
        a('dispose', fiber='p1'), a('settle', label='gap'), a('mount', fiber='p2', plugin='provider', config={'v': 'new'}), a('settle', label='new')] + teardown('c', 'p2'),
        [state('gap', 1, 'PENDING'), output('new', 'seen', 'new')])
    emit(16, 'Diamond dependency invalidation propagates through both branches', ['chain-invalidation', 'diamond-invalidation'], [
        plugin('root-provider', [a('provide', key='a', value={'$config': 'v'})]),
        plugin('left', [a('read', key='a', to='left'), a('provide', key='b', value={'$output': 'left'})], needs={'a': None}),
        plugin('right', [a('read', key='a', to='right'), a('provide', key='c', value={'$output': 'right'})], needs={'a': None}),
        plugin('leaf', [a('read', key='b', to='leaf-left'), a('read', key='c', to='leaf-right')], needs={'b': None, 'c': None})
    ], [a('mount', fiber=f, plugin=p, config=c) for f, p, c in [('p', 'root-provider', {'v': 1}), ('l', 'left', {}), ('r', 'right', {}), ('leaf', 'leaf', {})]] +
        [a('settle', label='before'), a('update', fiber='p', config={'v': 2}), a('settle', label='after')] + teardown('leaf', 'r', 'l', 'p'),
        [output('after', 'leaf-left', 2), output('after', 'leaf-right', 2)])
    emit(17, 'Updates coalesce to latest target while teardown is blocked', ['coalesced-updates'], [
        plugin('changing', [a('mark', label='apply', value={'$config': 'v'}), a('output', to='latest', value={'$config': 'v'})], [a('gate', label='teardown')])
    ], [a('mount', fiber='f', plugin='changing', config={'v': 1}), a('settle', label='initial'),
        a('update', fiber='f', config={'v': 2}, **{'await': False}, task='u2'), a('waitMark', label='teardown'),
        a('update', fiber='f', config={'v': 3}, **{'await': False}, task='u3'), a('release', label='teardown'), a('settle', label='latest')] + teardown('f'),
        [state('latest', 0, 'ACTIVE'), output('latest', 'latest', 3)], attribution=[CORE])
