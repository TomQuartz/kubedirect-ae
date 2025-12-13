#!/usr/bin/env python3
"""
Plotting script for trace experiment results (Figure 12-13).

Usage: python plot.py <run_id>
  run_id: the experiment run identifier (e.g., test)
"""

import os
import re
import sys
import pandas as pd
import numpy as np
import matplotlib.pyplot as plt

# Common plot settings (matching original notebook)
FONT_SIZE = 16
plt.rc('font', **{'size': FONT_SIZE, 'family': 'Arial'})
plt.rc('pdf', fonttype=42)

# Color scheme (matching original notebook)
COLORS = {
    'Kn/K8s': 'tab:green',
    'Kn/Kd': 'tab:blue',
    'Dirigent': 'tab:orange',
    'Dr/K8s+': 'tab:green',
    'Dr/Kd+': 'tab:blue',
}


def cdf(df, column):
    """Calculate CDF for a given column (matching original notebook logic)."""
    print(f"  p99: {df[column].quantile(0.99):.2f}")
    print(f"  p50: {df[column].quantile(0.50):.2f}")

    df_copy = df.copy()
    df_copy['density'] = 1 / df_copy.shape[0]
    df_grouped = df_copy.groupby(column).sum(numeric_only=True)

    res = df_grouped['density'].cumsum()

    # Make sure CDF starts from zero
    index = res.index[0]
    if index > 0:
        res.loc[0] = 0.0
        res = res.sort_index()

    return res


def get_slowdown_curve(path, is_csv=True, filter_timeout=False):
    """
    Calculate per-function average slowdown CDF.
    Matches getCurve() and getCurve2() from original notebook.

    Args:
        path: Path to data file
        is_csv: Whether the file is CSV format
        filter_timeout: If True, filter timeout invocations (latency > 8000ms)
    """
    print(f"\nProcessing: {path}")

    if is_csv:
        # CSV format (Kn/K8s, Dirigent)
        df = pd.read_csv(path)

        # Discard first 10 minutes
        df = df[~df['invocationID'].str.contains(r"^min[0-9]\.", regex=True)]

        print(f"  Failure percentage: {df[df['connectionTimeout'] | df['functionTimeout']].shape[0] / df.shape[0]:.4f}")

        # Extract function hash
        df['function_hash'] = df['instance'].str.split("-").str[0]
        df = df.reset_index(drop=True)

        # Calculate slowdown
        df['slowdown'] = df['responseTime'] / df['requestedDuration']
        print(f"  Slowdown < 1: {df[df['slowdown'] < 1].shape[0] / df.shape[0]:.4f}")
        print(f"  Number of instances created: {df['instance'].nunique()}")

        # Filter invalid slowdown and timeouts
        df = df[df['slowdown'] >= 1]
        df = df[df['connectionTimeout'] == False]
        df = df[df['functionTimeout'] == False]

        # Calculate per-function invocation count and filter functions with only 1 invocation
        func_counts = df.groupby('function_hash').size()
        valid_functions = func_counts[func_counts > 1].index
        df = df[df['function_hash'].isin(valid_functions)]
        print(f"  Number of functions with >1 invocation: {len(valid_functions)}")

        # Calculate per-function average slowdown
        df_grouped = df.groupby('function_hash')['slowdown'].mean().to_frame()
        print(f"  Number of functions: {df_grouped.size}")

    else:
        # Log format (Kn/Kd, Dr/K8s+, Dr/Kd+)
        pattern = r"ID: default/trace-(\d+)-(\d+)/(\d+),.*TS: ([\d.]+)s,.*Delay: \+([\d.]+)ms,.*Runtime: ([\d.]+)/(\d+)ms"
        data_list = []

        with open(path, 'r') as file:
            for line in file:
                match = re.match(pattern, line.strip())
                if match:
                    data = {
                        "ID": int(match.group(1)),
                        "minute": int(match.group(2)),
                        "invocationID": int(match.group(3)),
                        "TS": float(match.group(4)),
                        "Delay": float(match.group(5)),
                        "ActualRuntime": float(match.group(6)),
                        "RequestedRuntime": int(match.group(7)),
                    }
                    data_list.append(data)

        df = pd.DataFrame(data_list)

        # Discard first 10 minutes (TS >= 600s)
        df = df[df["TS"] >= 600]

        # Filter timeout invocations if requested (latency > 8000ms)
        if filter_timeout:
            original_count = len(df)
            df = df[df['Delay'] <= 8000]
            filtered_count = original_count - len(df)
            if filtered_count > 0:
                print(f"  Filtered {filtered_count} timeout invocations (latency > 8000ms, {filtered_count/original_count*100:.2f}%)")

        # Calculate slowdown
        df['slowdown'] = (df['Delay'] + df['ActualRuntime']) / df['RequestedRuntime']
        print(f"  Slowdown < 1: {df[df['slowdown'] < 1].shape[0] / df.shape[0]:.4f}")

        # Filter invalid slowdown
        df = df[df['slowdown'] >= 1]

        # Filter functions with only 1 invocation
        func_counts = df.groupby('ID').size()
        valid_functions = func_counts[func_counts > 1].index
        df = df[df['ID'].isin(valid_functions)]
        print(f"  Number of functions with >1 invocation: {len(valid_functions)}")

        # Calculate per-function average slowdown
        df_grouped = df.groupby('ID')['slowdown'].mean().to_frame()
        print(f"  Number of functions: {df_grouped.size}")

    print(f"  Calculating slowdown CDF...")
    return cdf(df_grouped, 'slowdown')


def get_scheduling_latency_curve(path, is_csv=True, filter_outliers=False, filter_timeout=False):
    """
    Calculate per-function average scheduling latency CDF.
    Matches get_per_function(), get_per_function2(), get_per_function3() from original notebook.

    Args:
        path: Path to data file
        is_csv: Whether the file is CSV format
        filter_outliers: If True, filter outliers < 1500ms (for Dirigent)
        filter_timeout: If True, filter timeout invocations (latency > 8000ms)
    """
    if is_csv:
        # CSV format
        df = pd.read_csv(path)

        # Discard first 10 minutes
        df = df[~df['invocationID'].str.contains(r"^min[0-9]\.", regex=True)]

        # Calculate scheduling latency: responseTime - actualDuration (in microseconds)
        df['sched_latency'] = df['responseTime'] - df['actualDuration']
        df['sched_latency'] /= 1000  # Convert to ms

        # Optional: filter outliers (for Dirigent in right subplot)
        if filter_outliers:
            df = df[df['sched_latency'] < 1500]

        # Remove instance suffix (deployment-xxx)
        df = df.apply(lambda x: x.replace({r'(\-[0-9]+(\-[0-9]+\-deployment\-[0-9A-Za-z\-]+){0,1})$': ""}, regex=True))

        # Filter instances with only 1 invocation
        instance_counts = df.groupby('instance').size()
        valid_instances = instance_counts[instance_counts > 1].index
        df = df[df['instance'].isin(valid_instances)]

        # Calculate per-instance average scheduling latency
        df_grouped = df.groupby('instance')['sched_latency'].mean().to_frame()

    else:
        # Log format
        pattern = r"ID: default/trace-(\d+)-(\d+)/(\d+),.*TS: ([\d.]+)s,.*Delay: \+([\d.]+)ms,.*Runtime: ([\d.]+)/(\d+)ms"
        data_list = []

        with open(path, 'r') as file:
            for line in file:
                match = re.match(pattern, line.strip())
                if match:
                    data = {
                        "ID": int(match.group(1)),
                        "minute": int(match.group(2)),
                        "invocationID": int(match.group(3)),
                        "TS": float(match.group(4)),
                        "Delay": float(match.group(5)),
                        "ActualRuntime": float(match.group(6)),
                        "RequestedRuntime": int(match.group(7)),
                    }
                    data_list.append(data)

        df = pd.DataFrame(data_list)

        # Discard first 10 minutes (TS >= 600s)
        df = df[df["TS"] >= 600]

        # Filter timeout invocations if requested (latency > 8000ms)
        if filter_timeout:
            original_count = len(df)
            df = df[df['Delay'] <= 8000]
            filtered_count = original_count - len(df)
            if filtered_count > 0:
                print(f"  Filtered {filtered_count} timeout invocations (latency > 8000ms, {filtered_count/original_count*100:.2f}%)")

        # Filter functions with only 1 invocation
        func_counts = df.groupby('ID').size()
        valid_functions = func_counts[func_counts > 1].index
        df = df[df['ID'].isin(valid_functions)]

        # Calculate per-function average delay (scheduling latency)
        df_grouped = df.groupby('ID')['Delay'].mean().to_frame()
        df_grouped.columns = ['sched_latency']

    print(f"  Calculating scheduling latency CDF...")
    return cdf(df_grouped, 'sched_latency')


def plot_dual_cdf(slowdown_data, sched_data, output_path, title_prefix=''):
    """
    Plot dual CDF subplots (slowdown and scheduling latency).
    Matches the original notebook's dual subplot layout.
    """
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(10, 4.5))

    # Plot slowdown CDF (left subplot)
    for label, cdf_curve in slowdown_data.items():
        if cdf_curve is not None:
            color = COLORS.get(label, 'gray')
            ax1.plot(cdf_curve, label=label, color=color)

    # Plot scheduling latency CDF (right subplot)
    for label, cdf_curve in sched_data.items():
        if cdf_curve is not None:
            color = COLORS.get(label, 'gray')
            ax2.plot(cdf_curve, label=label, color=color)

    # Configure left subplot (slowdown)
    ax1.set_xlabel("Avg. Per-Function Slowdown", fontsize=FONT_SIZE)
    ax1.set_ylabel("CDF")
    ax1.set_xscale('log')
    ax1.set_xticks([1, 10, 100, 1_000, 10_000, 100_000])
    ax1.set_xlim([1, 10 ** 4])
    ax1.grid()

    # Configure right subplot (scheduling latency)
    ax2.set_xlabel('Avg. Per-Function Scheduling Latency [ms]', fontsize=FONT_SIZE)
    ax2.set_ylabel('CDF')
    ax2.set_xscale('log')
    ax2.set_xticks([1, 10, 100, 1_000, 10_000, 100_000, 1_000_000])
    ax2.set_xlim([1, 10 ** 6])
    ax2.grid()

    # Add legends
    legend_length = 1.5
    bbox_to_anchor = (0.98, 0.02)
    handle_fontsize = FONT_SIZE
    ax1.legend(fontsize=handle_fontsize, handlelength=legend_length, ncol=1,
               loc='lower right', bbox_to_anchor=bbox_to_anchor, frameon=True,
               shadow=False, handletextpad=0.5, borderaxespad=0.)
    ax2.legend(fontsize=handle_fontsize, handlelength=legend_length, ncol=1,
               loc='lower right', bbox_to_anchor=bbox_to_anchor, frameon=True,
               shadow=False, handletextpad=0.5, borderaxespad=0.)

    fig.tight_layout()
    fig.subplots_adjust(wspace=0.25)

    plt.savefig(output_path, bbox_inches='tight', transparent=True)
    plt.close()
    print(f"\n✓ Saved figure to: {output_path}")


def main():
    if len(sys.argv) < 2:
        print("Usage: python plot.py <run_id>")
        sys.exit(1)

    run_id = sys.argv[1]
    base_dir = os.path.dirname(os.path.abspath(__file__))
    results_dir = os.path.join(base_dir, 'results')

    # Output directory: results/figures/${RUN_ID}/
    output_dir = os.path.join(results_dir, 'figures', run_id)
    os.makedirs(output_dir, exist_ok=True)

    print("=" * 70)
    print(f"Trace Experiment Plotting - Run ID: {run_id}")
    print("Filtering timeout invocations (latency > 8000ms) for Kn/Kd only")
    print("=" * 70)

    # Define file paths
    kn_k8s_path = os.path.join(results_dir, 'k8s', 'default', 'k8s.500.csv')
    dirigent_path = os.path.join(results_dir, 'dirigent', 'default', 'dirigent.500.csv')
    kn_kd_path = os.path.join(results_dir, 'kd', run_id, 'kd.500.log')
    dr_k8s_plus_path = os.path.join(results_dir, 'k8s+', run_id, 'k8s+.500.log')
    dr_kd_plus_path = os.path.join(results_dir, 'kd+', run_id, 'kd+.500.log')

    # ========================================================================
    # Figure 12: Knative comparison (Kn/K8s vs Kn/Kd)
    # ========================================================================
    print("\n" + "=" * 70)
    print("FIGURE 12: Knative Comparison (Kn/K8s vs Kn/Kd)")
    print("=" * 70)

    fig12_slowdown = {}
    fig12_sched = {}

    if os.path.exists(kn_k8s_path):
        print("\n[Kn/K8s]")
        fig12_slowdown['Kn/K8s'] = get_slowdown_curve(kn_k8s_path, is_csv=True, filter_timeout=False)
        fig12_sched['Kn/K8s'] = get_scheduling_latency_curve(kn_k8s_path, is_csv=True, filter_outliers=False, filter_timeout=False)
    else:
        print(f"\nWarning: {kn_k8s_path} not found")

    if os.path.exists(kn_kd_path):
        print(f"\n[Kn/Kd] (filtering timeout invocations)")
        fig12_slowdown['Kn/Kd'] = get_slowdown_curve(kn_kd_path, is_csv=False, filter_timeout=True)
        fig12_sched['Kn/Kd'] = get_scheduling_latency_curve(kn_kd_path, is_csv=False, filter_timeout=True)
    else:
        print(f"\nWarning: {kn_kd_path} not found")

    if fig12_slowdown and fig12_sched:
        plot_dual_cdf(
            fig12_slowdown,
            fig12_sched,
            os.path.join(output_dir, 'knative.pdf'),
            title_prefix='Figure 12'
        )
    else:
        print("\nWarning: Insufficient data for Figure 12")

    # ========================================================================
    # Figure 13: Dirigent comparison (Dirigent vs Dr/K8s+ vs Dr/Kd+)
    # ========================================================================
    print("\n" + "=" * 70)
    print("FIGURE 13: Dirigent Comparison (Dirigent vs Dr/K8s+ vs Dr/Kd+)")
    print("=" * 70)

    fig13_slowdown = {}
    fig13_sched = {}

    if os.path.exists(dirigent_path):
        print("\n[Dirigent]")
        fig13_slowdown['Dirigent'] = get_slowdown_curve(dirigent_path, is_csv=True, filter_timeout=False)
        # Note: Dirigent uses filter_outliers=True for scheduling latency (< 1500ms)
        fig13_sched['Dirigent'] = get_scheduling_latency_curve(dirigent_path, is_csv=True, filter_outliers=True, filter_timeout=False)
    else:
        print(f"\nWarning: {dirigent_path} not found")

    if os.path.exists(dr_k8s_plus_path):
        print("\n[Dr/K8s+]")
        fig13_slowdown['Dr/K8s+'] = get_slowdown_curve(dr_k8s_plus_path, is_csv=False, filter_timeout=False)
        fig13_sched['Dr/K8s+'] = get_scheduling_latency_curve(dr_k8s_plus_path, is_csv=False, filter_timeout=False)
    else:
        print(f"\nWarning: {dr_k8s_plus_path} not found")

    if os.path.exists(dr_kd_plus_path):
        print(f"\n[Dr/Kd+]")
        fig13_slowdown['Dr/Kd+'] = get_slowdown_curve(dr_kd_plus_path, is_csv=False, filter_timeout=False)
        fig13_sched['Dr/Kd+'] = get_scheduling_latency_curve(dr_kd_plus_path, is_csv=False, filter_timeout=False)
    else:
        print(f"\nWarning: {dr_kd_plus_path} not found")

    if fig13_slowdown and fig13_sched:
        plot_dual_cdf(
            fig13_slowdown,
            fig13_sched,
            os.path.join(output_dir, 'dirigent.pdf'),
            title_prefix='Figure 13'
        )
    else:
        print("\nWarning: Insufficient data for Figure 13")


if __name__ == '__main__':
    main()
