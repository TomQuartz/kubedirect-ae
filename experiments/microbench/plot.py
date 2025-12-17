#!/usr/bin/env python3
"""
Plotting script for microbenchmark results.
Usage: python plot.py <benchmark> <run_id>
  benchmark: scale-pods, scale-funcs, or scale-nodes
  run_id: the experiment run identifier (e.g., test_1)
"""

import os
import re
import sys
import numpy as np
import matplotlib.pyplot as plt

# Common plot settings
FONT_SIZE = 18
FONT_SIZE_SMALL = 16
plt.rc('font', **{'size': FONT_SIZE, 'family': 'Arial'})
plt.rc('pdf', fonttype=42)

COLORS = {
    'K8s': ("lightcoral", 1., None),
    'K8s+': ("lightcoral", 0.8, '/'),
    'Kd': ("lightsalmon", 1., None),
    'Kd+': ("lightsalmon", 0.8, '//'),
    'K8s(K8s+)': ("lightcoral", 1., None),
    'Kd(Kd+)': ("lightsalmon", 1., None),
    'K8s(Kd)': ("lightcoral", 1., None),
    'K8s+(Kd+)': ("lightsalmon", 0.8, '//'),
    'E2E': ("coral", 1., None),
    'Scheduler': ("salmon", 0.6, None),
    'Sandbox Mgr.': ("salmon", 0.4, None),
}


def parse_log(filepath):
    """Parse log file and extract total time in microseconds."""
    if not os.path.exists(filepath):
        return None
    with open(filepath, 'r') as f:
        content = f.read()
    match = re.search(r'total:\s*(\d+)\s*us', content)
    if match:
        return int(match.group(1))
    return None


def us_to_sec(us):
    """Convert microseconds to seconds."""
    if us is None:
        return None
    return us / 1_000_000


def load_data(results_dir, prefix, baselines, scales):
    """Load data from log files for given baselines and scales."""
    data = {b: [] for b in baselines}
    for baseline in baselines:
        for scale in scales:
            filepath = os.path.join(results_dir, f"{prefix}.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data[baseline].append(val if val is not None else 0)
    return data


def plot_broken_axis_vertical(data, settings, output_path, top_lim, bottom_lim,
                               fig_size=(5, 3), bottom_factor=2, width=0.15,
                               top_yticks=None, top_ylabels=None,
                               bottom_yticks=None, bottom_ylabels=None,
                               xlabel=None, legend_fontsize=14):
    """Plot bar chart with broken y-axis (vertical split)."""
    baselines = list(data.keys())
    n_baselines = len(baselines)
    n_settings = len(settings)
    x = np.arange(n_settings)

    fig, (ax1, ax2) = plt.subplots(2, 1, sharex=True, figsize=fig_size,
                                    gridspec_kw={'height_ratios': [1, bottom_factor]})

    ax1.set_ylim(*top_lim)
    ax2.set_ylim(*bottom_lim)

    for i, baseline in enumerate(baselines):
        scores = np.array(data[baseline])
        color = COLORS.get(baseline, ("gray", 1., None))
        ax2.bar(x + i * width, scores, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')
        scores_top = scores.copy()
        scores_top[scores_top < top_lim[0]] = 0
        ax1.bar(x + i * width, scores_top, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    # Hide spines between ax1 and ax2
    ax1.spines['bottom'].set_visible(False)
    ax2.spines['top'].set_visible(False)
    ax1.tick_params(labeltop=False)
    ax1.tick_params(axis='x', length=0)

    if top_yticks is not None:
        ax1.set_yticks(top_yticks)
    if top_ylabels is not None:
        ax1.set_yticklabels(top_ylabels)

    # Diagonal lines for broken axis
    d = .01
    kwargs = dict(transform=ax1.transAxes, color='k', clip_on=False)
    ax1.plot((-d, +d), (-d*bottom_factor, +d*bottom_factor), **kwargs)
    ax1.plot((1 - d, 1 + d), (-d*bottom_factor, +d*bottom_factor), **kwargs)

    kwargs.update(transform=ax2.transAxes)
    ax2.plot((-d, +d), (1 - d, 1 + d), **kwargs)
    ax2.plot((1 - d, 1 + d), (1 - d, 1 + d), **kwargs)

    if bottom_yticks is not None:
        ax2.set_yticks(bottom_yticks)
    if bottom_ylabels is not None:
        ax2.set_yticklabels(bottom_ylabels)

    if xlabel:
        ax2.set_xticks([])
        ax2.set_xlabel(xlabel, fontsize=FONT_SIZE, labelpad=10)
    else:
        ax2.set_xticks(x + width * (n_baselines - 1) / 2)
        ax2.set_xticklabels(settings)

    legend_length = 1.
    bbox_to_anchor = (0.5, 1)
    ax1.legend(fontsize=legend_fontsize, handlelength=legend_length, ncol=len(baselines),
               loc='lower center', bbox_to_anchor=bbox_to_anchor, frameon=False,
               shadow=False, handletextpad=0.3, columnspacing=0.8, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(hspace=0.1)
    plt.savefig(output_path, bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {output_path}")


def plot_simple_bar(data, settings, output_path, fig_size=(4, 3.5), width=0.25,
                    yticks=None, ylabels=None, xlabel=None, legend_fontsize=18):
    """Plot simple bar chart without broken axis."""
    baselines = list(data.keys())
    n_baselines = len(baselines)
    n_settings = len(settings)
    x = np.arange(n_settings)

    fig, ax = plt.subplots(1, 1, figsize=fig_size)

    for i, baseline in enumerate(baselines):
        scores = np.array(data[baseline])
        color = COLORS.get(baseline, ("gray", 1., None))
        ax.bar(x + i * width, scores, width, label=baseline,
               color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    if yticks is not None:
        ax.set_yticks(yticks)
    if ylabels is not None:
        ax.set_yticklabels(ylabels)

    if xlabel:
        ax.set_xticks([])
        ax.set_xlabel(xlabel, fontsize=FONT_SIZE, labelpad=10)
    else:
        ax.set_xticks(x + width * (n_baselines - 1) / 2)
        ax.set_xticklabels(settings)

    legend_length = 1
    bbox_to_anchor = (0.5, 1)
    ax.legend(fontsize=legend_fontsize, handlelength=legend_length, ncol=len(baselines),
              loc='lower center', bbox_to_anchor=bbox_to_anchor, frameon=False,
              shadow=False, handletextpad=0.5, columnspacing=0.5, borderaxespad=0.)

    fig.tight_layout()
    plt.savefig(output_path, bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {output_path}")


def plot_scale_pods(results_dir):
    """Generate plots for scale-pods benchmark."""
    # Detect available scales from log files
    scales = []
    for f in os.listdir(results_dir):
        if f.startswith('e2e.k8s.') and f.endswith('.log'):
            scale = f.replace('e2e.k8s.', '').replace('.log', '')
            if scale.isdigit():
                scales.append(int(scale))
    scales = sorted(scales)
    if not scales:
        print("No scale-pods data found")
        return

    settings = [f'N={s}' for s in scales]
    n_settings = len(settings)
    x = np.arange(n_settings)

    # 1. E2E plot (with 4 baselines)
    data_e2e = {}
    for baseline in ['k8s', 'k8s+', 'kd', 'kd+']:
        data_e2e[baseline] = []
        for scale in scales:
            filepath = os.path.join(results_dir, f"e2e.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data_e2e[baseline].append(val if val is not None else 0)

    data_e2e_display = {
        'K8s': data_e2e['k8s'],
        'K8s+': data_e2e['k8s+'],
        'Kd': data_e2e['kd'],
        'Kd+': data_e2e['kd+'],
    }

    baselines = list(data_e2e_display.keys())
    n_baselines = len(baselines)
    width = 0.15

    fig, (ax1, ax2) = plt.subplots(2, 1, sharex=True, figsize=(5, 3),
                                    gridspec_kw={'height_ratios': [1, 2]})

    top_lim = (5, 50)
    bottom_lim = (0, 4.9)
    ax1.set_ylim(*top_lim)
    ax2.set_ylim(*bottom_lim)

    for i, baseline in enumerate(baselines):
        scores = np.array(data_e2e_display[baseline])
        color = COLORS[baseline]
        ax2.bar(x + i * width, scores, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')
        scores_top = scores.copy()
        scores_top[scores_top < top_lim[0]] = 0
        ax1.bar(x + i * width, scores_top, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    ax1.spines['bottom'].set_visible(False)
    ax2.spines['top'].set_visible(False)
    ax1.tick_params(labeltop=False)
    ax1.tick_params(axis='x', length=0)
    ax1.set_yticks(np.array([5, 25, 50]))
    ax1.set_yticklabels(['5s', '25s', '50s'])

    d = .01
    kwargs = dict(transform=ax1.transAxes, color='k', clip_on=False)
    ax1.plot((-d, +d), (-d*2, +d*2), **kwargs)
    ax1.plot((1 - d, 1 + d), (-d*2, +d*2), **kwargs)

    kwargs.update(transform=ax2.transAxes)
    ax2.plot((-d, +d), (1 - d, 1 + d), **kwargs)
    ax2.plot((1 - d, 1 + d), (1 - d, 1 + d), **kwargs)

    ax2.set_yticks(np.arange(0, 5, 1))
    ax2.set_yticklabels(['0s', '1s', '2s', '3s', '4s'])
    ax2.set_xticks(x + width * (n_baselines - 1) / 2)
    ax2.set_xticklabels(settings)

    ax1.legend(fontsize=14, handlelength=1., ncol=len(baselines),
               loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
               shadow=False, handletextpad=0.3, columnspacing=0.8, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(hspace=0.1)
    plt.savefig(os.path.join(results_dir, 'e2e.pdf'), bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {os.path.join(results_dir, 'e2e.pdf')}")

    
    # 2. ReplicaSet breakdown plot
    data_rs = {}
    for baseline in ['k8s', 'kd']:
        data_rs[baseline] = []
        for scale in scales:
            filepath = os.path.join(results_dir, f"_rs.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data_rs[baseline].append(val if val is not None else 0)

    data_rs_display = {
        'K8s(K8s+)': data_rs['k8s'],
        'Kd(Kd+)': data_rs['kd'],
    }

    baselines = list(data_rs_display.keys())
    n_baselines = len(baselines)
    x = np.arange(n_settings)
    width = 0.25

    fig, (ax1, ax2) = plt.subplots(2, 1, sharex=True, figsize=(4, 3.5),
                                    gridspec_kw={'height_ratios': [1, 2]})

    top_lim = (1, 50)
    bottom_lim = (0, 12e-3)
    ax1.set_ylim(*top_lim)
    ax2.set_ylim(*bottom_lim)

    for i, baseline in enumerate(baselines):
        scores = np.array(data_rs_display[baseline])
        color = COLORS[baseline]
        ax2.bar(x + i * width, scores, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')
        scores_top = scores.copy()
        scores_top[scores_top < top_lim[0]] = 0
        ax1.bar(x + i * width, scores_top, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    ax1.spines['bottom'].set_visible(False)
    ax2.spines['top'].set_visible(False)
    ax1.tick_params(labeltop=False)
    ax1.tick_params(axis='x', length=0)
    ax1.set_yticks(np.array([1, 25, 50]))
    ax1.set_yticklabels(['1s', '25s', '50s'])

    d = .02
    kwargs = dict(transform=ax1.transAxes, color='k', clip_on=False)
    ax1.plot((-d, +d), (-d*2, +d*2), **kwargs)
    ax1.plot((1 - d, 1 + d), (-d*2, +d*2), **kwargs)

    kwargs.update(transform=ax2.transAxes)
    ax2.plot((-d, +d), (1 - d, 1 + d), **kwargs)
    ax2.plot((1 - d, 1 + d), (1 - d, 1 + d), **kwargs)

    ax2.set_yticks(np.arange(0, 15e-3, 5e-3))
    ax2.set_yticklabels(['0', '5ms', '10ms'])
    ax2.set_xticks([])
    ax2.set_xlabel(f"N={scales[0]}   $\\longrightarrow$   N={scales[-1]}",
                   fontsize=FONT_SIZE, labelpad=10)

    ax1.legend(fontsize=18, handlelength=1, ncol=len(baselines),
               loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
               shadow=False, handletextpad=0.5, columnspacing=0.5, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(hspace=0.08)
    plt.savefig(os.path.join(results_dir, 'rs.pdf'), bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {os.path.join(results_dir, 'rs.pdf')}")

    
    # 3. Scheduler breakdown plot
    data_sched = {}
    for baseline in ['k8s', 'kd']:
        data_sched[baseline] = []
        for scale in scales:
            filepath = os.path.join(results_dir, f"_sched.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data_sched[baseline].append(val if val is not None else 0)

    data_sched_display = {
        'K8s(K8s+)': data_sched['k8s'],
        'Kd(Kd+)': data_sched['kd'],
    }

    baselines = list(data_sched_display.keys())
    fig, (ax1, ax2) = plt.subplots(2, 1, sharex=True, figsize=(4, 3.5),
                                    gridspec_kw={'height_ratios': [1, 2]})

    top_lim = (1, 20)
    bottom_lim = (0, 0.7)
    ax1.set_ylim(*top_lim)
    ax2.set_ylim(*bottom_lim)

    for i, baseline in enumerate(baselines):
        scores = np.array(data_sched_display[baseline])
        color = COLORS[baseline]
        ax2.bar(x + i * width, scores, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')
        scores_top = scores.copy()
        scores_top[scores_top < top_lim[0]] = 0
        ax1.bar(x + i * width, scores_top, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    ax1.spines['bottom'].set_visible(False)
    ax2.spines['top'].set_visible(False)
    ax1.tick_params(labeltop=False)
    ax1.tick_params(axis='x', length=0)
    ax1.set_yticks(np.array([1, 10, 20]))
    ax1.set_yticklabels(['1s', '10s', '20s'])

    d = .02
    kwargs = dict(transform=ax1.transAxes, color='k', clip_on=False)
    ax1.plot((-d, +d), (-d*2, +d*2), **kwargs)
    ax1.plot((1 - d, 1 + d), (-d*2, +d*2), **kwargs)

    kwargs.update(transform=ax2.transAxes)
    ax2.plot((-d, +d), (1 - d, 1 + d), **kwargs)
    ax2.plot((1 - d, 1 + d), (1 - d, 1 + d), **kwargs)

    ax2.set_yticks(np.arange(0, 0.8, 0.2))
    ax2.set_yticklabels(['0', '0.2s', '0.4s', '0.6s'])
    ax2.set_xticks([])
    ax2.set_xlabel(f"N={scales[0]}   $\\longrightarrow$   N={scales[-1]}",
                   fontsize=FONT_SIZE, labelpad=10)

    ax1.legend(fontsize=18, handlelength=1, ncol=len(baselines),
               loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
               shadow=False, handletextpad=0.5, columnspacing=0.5, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(hspace=0.08)
    plt.savefig(os.path.join(results_dir, 'sched.pdf'), bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {os.path.join(results_dir, 'sched.pdf')}")

    # 4. Runtime (kubelet) breakdown plot
    data_runtime = {}
    for baseline in ['kubelet', 'custom']:
        data_runtime[baseline] = []
        for scale in scales:
            filepath = os.path.join(results_dir, f"_runtime.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data_runtime[baseline].append(val if val is not None else 0)

    data_runtime_display = {
        'K8s(Kd)': data_runtime['kubelet'],
        'K8s+(Kd+)': data_runtime['custom'],
    }

    baselines = list(data_runtime_display.keys())
    fig, ax = plt.subplots(1, 1, figsize=(4, 3.5))

    for i, baseline in enumerate(baselines):
        scores = np.array(data_runtime_display[baseline])
        color = COLORS[baseline]
        ax.bar(x + i * width, scores, width, label=baseline,
               color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    ax.set_yticks(np.arange(0, 1, 0.2))
    ax.set_yticklabels(['0', '0.2s', '0.4s', '0.6s', '0.8s'])
    ax.set_xticks([])
    ax.set_xlabel(f"N={scales[0]}   $\\longrightarrow$   N={scales[-1]}",
                  fontsize=FONT_SIZE, labelpad=10)

    ax.legend(fontsize=18, handlelength=1, ncol=len(baselines),
              loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
              shadow=False, handletextpad=0.5, columnspacing=0.5, borderaxespad=0.)

    fig.tight_layout()
    plt.savefig(os.path.join(results_dir, 'runtime.pdf'), bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {os.path.join(results_dir, 'runtime.pdf')}")


def  plot_scale_funcs(results_dir):
    """Generate plots for scale-funcs benchmark."""
    # Detect available scales from log files
    scales = []
    for f in os.listdir(results_dir):
        if f.startswith('e2e.k8s.') and f.endswith('.log'):
            scale = f.replace('e2e.k8s.', '').replace('.log', '')
            if scale.isdigit():
                scales.append(int(scale))
    scales = sorted(scales)
    if not scales:
        print("No scale-funcs data found")
        return

    settings = [f'K={s}' for s in scales]
    n_settings = len(settings)
    x = np.arange(n_settings)

    # 1. E2E plot (e2e.long.pdf)
    data_e2e = {}
    for baseline in ['k8s', 'k8s+', 'kd', 'kd+']:
        data_e2e[baseline] = []
        for scale in scales:
            filepath = os.path.join(results_dir, f"e2e.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data_e2e[baseline].append(val if val is not None else 0)

    data_e2e_display = {
        'K8s': data_e2e['k8s'],
        'K8s+': data_e2e['k8s+'],
        'Kd': data_e2e['kd'],
        'Kd+': data_e2e['kd+'],
    }

    baselines = list(data_e2e_display.keys())
    n_baselines = len(baselines)
    width = 0.15

    fig, (ax1, ax2) = plt.subplots(2, 1, sharex=True, figsize=(10, 3.5),
                                    gridspec_kw={'height_ratios': [1, 2]})

    top_lim = (5, 110)
    bottom_lim = (0, 3.5)
    ax1.set_ylim(*top_lim)
    ax2.set_ylim(*bottom_lim)

    for i, baseline in enumerate(baselines):
        scores = np.array(data_e2e_display[baseline])
        color = COLORS[baseline]
        ax2.bar(x + i * width, scores, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')
        scores_top = scores.copy()
        scores_top[scores_top < top_lim[0]] = 0
        ax1.bar(x + i * width, scores_top, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    ax1.spines['bottom'].set_visible(False)
    ax2.spines['top'].set_visible(False)
    ax1.tick_params(labeltop=False)
    ax1.tick_params(axis='x', length=0)
    ax1.set_yticks(np.array([5, 50, 100]))
    ax1.set_yticklabels(['5s', '50s', '100s'])

    d = .01
    kwargs = dict(transform=ax1.transAxes, color='k', clip_on=False)
    ax1.plot((-d, +d), (-d*2, +d*2), **kwargs)
    ax1.plot((1 - d, 1 + d), (-d*2, +d*2), **kwargs)

    kwargs.update(transform=ax2.transAxes)
    ax2.plot((-d, +d), (1 - d, 1 + d), **kwargs)
    ax2.plot((1 - d, 1 + d), (1 - d, 1 + d), **kwargs)

    ax2.set_yticks(np.arange(0, 4, 1))
    ax2.set_yticklabels(['0', '1s', '2s', '3s'])
    ax2.set_xticks(x + width * (n_baselines - 1) / 2)
    ax2.set_xticklabels(settings)

    ax1.legend(fontsize=18, handlelength=1.5, ncol=len(baselines),
               loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
               shadow=False, handletextpad=0.5, columnspacing=2, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(hspace=0.08)
    plt.savefig(os.path.join(results_dir, 'e2e.long.pdf'), bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {os.path.join(results_dir, 'e2e.long.pdf')}")

    # 2. Autoscaler breakdown plot
    data_as = {}
    for baseline in ['k8s', 'kd']:
        data_as[baseline] = []
        for scale in scales:
            filepath = os.path.join(results_dir, f"_as.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data_as[baseline].append(val if val is not None else 0)

    data_as_display = {
        'K8s(K8s+)': data_as['k8s'],
        'Kd(Kd+)': data_as['kd'],
    }

    baselines = list(data_as_display.keys())
    n_baselines = len(baselines)
    width = 0.25

    fig, (ax1, ax2) = plt.subplots(2, 1, sharex=True, figsize=(4, 3.5),
                                    gridspec_kw={'height_ratios': [1, 1]})

    top_lim = (2, 80)
    bottom_lim = (0, 1.5)
    ax1.set_ylim(*top_lim)
    ax2.set_ylim(*bottom_lim)

    for i, baseline in enumerate(baselines):
        scores = np.array(data_as_display[baseline])
        color = COLORS[baseline]
        ax2.bar(x + i * width, scores, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')
        scores_top = scores.copy()
        scores_top[scores_top < top_lim[0]] = 0
        ax1.bar(x + i * width, scores_top, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    ax1.spines['bottom'].set_visible(False)
    ax2.spines['top'].set_visible(False)
    ax1.tick_params(labeltop=False)
    ax1.tick_params(axis='x', length=0)
    ax1.set_yticks(np.array([2, 40, 80]))
    ax1.set_yticklabels(['2s', '40s', '80s'])

    d = .02
    kwargs = dict(transform=ax1.transAxes, color='k', clip_on=False)
    ax1.plot((-d, +d), (-d, +d), **kwargs)
    ax1.plot((1 - d, 1 + d), (-d, +d), **kwargs)

    kwargs.update(transform=ax2.transAxes)
    ax2.plot((-d, +d), (1 - d, 1 + d), **kwargs)
    ax2.plot((1 - d, 1 + d), (1 - d, 1 + d), **kwargs)

    ax2.set_yticks(np.arange(0, 1.5, 0.5))
    ax2.set_yticklabels(['0', '0.5s', '1s'])
    ax2.set_xticks([])
    ax2.set_xlabel(f"K={scales[0]}   $\\longrightarrow$   K={scales[-1]}",
                   fontsize=FONT_SIZE, labelpad=10)

    ax1.legend(fontsize=18, handlelength=1, ncol=len(baselines),
               loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
               shadow=False, handletextpad=0.5, columnspacing=0.5, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(hspace=0.08)
    plt.savefig(os.path.join(results_dir, 'as.pdf'), bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {os.path.join(results_dir, 'as.pdf')}")

    # 3. Deployment breakdown plot
    data_dp = {}
    for baseline in ['k8s', 'kd']:
        data_dp[baseline] = []
        for scale in scales:
            filepath = os.path.join(results_dir, f"_dp.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data_dp[baseline].append(val if val is not None else 0)

    data_dp_display = {
        'K8s(K8s+)': data_dp['k8s'],
        'Kd(Kd+)': data_dp['kd'],
    }

    baselines = list(data_dp_display.keys())
    fig, (ax1, ax2) = plt.subplots(2, 1, sharex=True, figsize=(4, 3.5),
                                    gridspec_kw={'height_ratios': [1, 1]})

    top_lim = (2, 80)
    bottom_lim = (0, 1.5)
    ax1.set_ylim(*top_lim)
    ax2.set_ylim(*bottom_lim)

    for i, baseline in enumerate(baselines):
        scores = np.array(data_dp_display[baseline])
        color = COLORS[baseline]
        ax2.bar(x + i * width, scores, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')
        scores_top = scores.copy()
        scores_top[scores_top < top_lim[0]] = 0
        ax1.bar(x + i * width, scores_top, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    ax1.spines['bottom'].set_visible(False)
    ax2.spines['top'].set_visible(False)
    ax1.tick_params(labeltop=False)
    ax1.tick_params(axis='x', length=0)
    ax1.set_yticks(np.array([2, 40, 80]))
    ax1.set_yticklabels(['2s', '40s', '80s'])

    d = .02
    kwargs = dict(transform=ax1.transAxes, color='k', clip_on=False)
    ax1.plot((-d, +d), (-d, +d), **kwargs)
    ax1.plot((1 - d, 1 + d), (-d, +d), **kwargs)

    kwargs.update(transform=ax2.transAxes)
    ax2.plot((-d, +d), (1 - d, 1 + d), **kwargs)
    ax2.plot((1 - d, 1 + d), (1 - d, 1 + d), **kwargs)

    ax2.set_yticks(np.arange(0, 1.5, 0.5))
    ax2.set_yticklabels(['0', '0.5s', '1s'])
    ax2.set_xticks([])
    ax2.set_xlabel(f"K={scales[0]}   $\\longrightarrow$   K={scales[-1]}",
                   fontsize=FONT_SIZE, labelpad=10)

    ax1.legend(fontsize=18, handlelength=1, ncol=len(baselines),
               loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
               shadow=False, handletextpad=0.5, columnspacing=0.5, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(hspace=0.08)
    plt.savefig(os.path.join(results_dir, 'dp.pdf'), bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {os.path.join(results_dir, 'dp.pdf')}")

    # 4. ReplicaSet breakdown plot
    data_rs = {}
    for baseline in ['k8s', 'kd']:
        data_rs[baseline] = []
        for scale in scales:
            filepath = os.path.join(results_dir, f"_rs.{baseline}.{scale}.log")
            val = us_to_sec(parse_log(filepath))
            data_rs[baseline].append(val if val is not None else 0)

    data_rs_display = {
        'K8s(K8s+)': data_rs['k8s'],
        'Kd(Kd+)': data_rs['kd'],
    }

    baselines = list(data_rs_display.keys())
    fig, (ax1, ax2) = plt.subplots(2, 1, sharex=True, figsize=(4, 3.5),
                                    gridspec_kw={'height_ratios': [1, 1]})

    top_lim = (2, 50)
    bottom_lim = (0, 1.5)
    ax1.set_ylim(*top_lim)
    ax2.set_ylim(*bottom_lim)

    for i, baseline in enumerate(baselines):
        scores = np.array(data_rs_display[baseline])
        color = COLORS[baseline]
        ax2.bar(x + i * width, scores, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')
        scores_top = scores.copy()
        scores_top[scores_top < top_lim[0]] = 0
        ax1.bar(x + i * width, scores_top, width, label=baseline,
                color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    ax1.spines['bottom'].set_visible(False)
    ax2.spines['top'].set_visible(False)
    ax1.tick_params(labeltop=False)
    ax1.tick_params(axis='x', length=0)
    ax1.set_yticks(np.array([2, 25, 50]))
    ax1.set_yticklabels(['2s', '25s', '50s'])

    d = .02
    kwargs = dict(transform=ax1.transAxes, color='k', clip_on=False)
    ax1.plot((-d, +d), (-d, +d), **kwargs)
    ax1.plot((1 - d, 1 + d), (-d, +d), **kwargs)

    kwargs.update(transform=ax2.transAxes)
    ax2.plot((-d, +d), (1 - d, 1 + d), **kwargs)
    ax2.plot((1 - d, 1 + d), (1 - d, 1 + d), **kwargs)

    ax2.set_yticks(np.arange(0, 1.5, 0.5))
    ax2.set_yticklabels(['0', '0.5s', '1s'])
    ax2.set_xticks([])
    ax2.set_xlabel(f"K={scales[0]}   $\\longrightarrow$   K={scales[-1]}",
                   fontsize=FONT_SIZE, labelpad=10)

    ax1.legend(fontsize=18, handlelength=1, ncol=len(baselines),
               loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
               shadow=False, handletextpad=0.5, columnspacing=0.5, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(hspace=0.08)
    plt.savefig(os.path.join(results_dir, 'rs.pdf'), bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {os.path.join(results_dir, 'rs.pdf')}")

    


def plot_scale_nodes(results_dir):
    """Generate plots for scale-nodes benchmark."""
    # Detect available scales from log files
    scales = []
    for f in os.listdir(results_dir):
        if f.startswith('e2e.kd+.') and f.endswith('.log'):
            scale = f.replace('e2e.kd+.', '').replace('.log', '')
            if scale.isdigit():
                scales.append(int(scale))
    scales = sorted(scales)
    if not scales:
        print("No scale-nodes data found")
        return

    # Format settings - always use K format (e.g., M=0.5K, M=1K, M=1.5K)
    settings = []
    for s in scales:
        k_val = s / 1000
        if k_val == int(k_val):
            settings.append(f'M={int(k_val)}K')
        else:
            settings.append(f'M={k_val:.1f}K')

    # Load data for E2E, Scheduler, and Runtime
    data_e2e = []
    data_sched = []
    data_runtime = []

    for scale in scales:
        # E2E uses kd+
        filepath = os.path.join(results_dir, f"e2e.kd+.{scale}.log")
        val = us_to_sec(parse_log(filepath))
        data_e2e.append(val if val is not None else 0)

        # Scheduler uses kd
        filepath = os.path.join(results_dir, f"_sched.kd.{scale}.log")
        val = us_to_sec(parse_log(filepath))
        data_sched.append(val if val is not None else 0)

        # Runtime uses custom
        filepath = os.path.join(results_dir, f"_runtime.custom.{scale}.log")
        if os.path.exists(filepath):
            val = us_to_sec(parse_log(filepath))
            data_runtime.append(val if val is not None else 0)
        else:
            data_runtime.append(0)

    data_dict = {
        'E2E': data_e2e,
        'Scheduler': data_sched,
        'Sandbox Mgr.': data_runtime,
    }

    baselines = list(data_dict.keys())
    n_baselines = len(baselines)
    n_settings = len(settings)
    x = np.arange(n_settings)
    width = 0.2

    fig, ax = plt.subplots(1, 1, figsize=(7, 3))

    for i, baseline in enumerate(baselines):
        scores = np.array(data_dict[baseline])
        color = COLORS[baseline]
        ax.bar(x + i * width, scores, width, label=baseline,
               color=color[0], alpha=color[1], hatch=color[2], edgecolor='black')

    # Set y-axis with fixed scale matching notebook
    ax.set_yticks(np.arange(0, 45, 10))
    ax.set_yticklabels(['0', '10s', '20s', '30s', '40s'])

    ax.set_xticks(x + width * (n_baselines - 1) / 2)
    ax.set_xticklabels(settings, fontsize=FONT_SIZE_SMALL)

    ax.legend(fontsize=FONT_SIZE_SMALL, handlelength=1, ncol=len(baselines),
              loc='lower center', bbox_to_anchor=(0.5, 1), frameon=False,
              shadow=False, handletextpad=0.5, columnspacing=2, borderaxespad=0.)

    fig.tight_layout()
    output_path = os.path.join(results_dir, 'all.pdf')
    plt.savefig(output_path, bbox_inches='tight', transparent=True)
    plt.close()
    print(f"Saved: {output_path}")


def main():
    if len(sys.argv) < 3:
        print("Usage: python plot.py <benchmark> <run_id>")
        print("  benchmark: scale-pods, scale-funcs, or scale-nodes")
        print("  run_id: the experiment run identifier (e.g., test_1)")
        sys.exit(1)

    benchmark = sys.argv[1]
    run_id = sys.argv[2]

    base_dir = os.path.dirname(os.path.abspath(__file__))
    results_dir = os.path.join(base_dir, 'results', benchmark, run_id)

    if not os.path.exists(results_dir):
        print(f"Results directory not found: {results_dir}")
        sys.exit(1)

    print(f"Plotting {benchmark} from {results_dir}")

    if benchmark == 'scale-pods':
        plot_scale_pods(results_dir)
    elif benchmark == 'scale-funcs':
        plot_scale_funcs(results_dir)
    elif benchmark == 'scale-nodes':
        plot_scale_nodes(results_dir)
    else:
        print(f"Unknown benchmark: {benchmark}")
        sys.exit(1)


if __name__ == '__main__':
    main()
