#!/usr/bin/env python3
"""
Script para corrigir o dashboard do Grafana removendo queries vazias
e ajustando para métricas que realmente existem.
"""
import json
import sys

# Métricas que realmente existem no Prometheus
AVAILABLE_METRICS = {
    'active_tasks',
    'containers_monitored',
    'cpu_usage_percent',
    'dispatcher_queue_utilization',
    'files_monitored',
    'goroutines',
    'logs_per_second',
    'logs_processed_total',
    'logs_sent_total',
    'memory_usage_bytes',
    'processing_duration_seconds_bucket',
    'processing_duration_seconds_count',
    'processing_duration_seconds_sum',
    'processing_step_duration_seconds_bucket',
    'processing_step_duration_seconds_count',
    'processing_step_duration_seconds_sum',
    'queue_size',
    'sink_queue_utilization',
    'sink_send_duration_seconds_bucket',
    'sink_send_duration_seconds_count',
    'sink_send_duration_seconds_sum',
    'gc_runs_total',
    # Métricas do Go padrão
    'go_memstats_alloc_bytes',
    'go_memstats_sys_bytes',
    'go_goroutines',
    'process_cpu_seconds_total',
    'up'
}

def fix_panel_targets(panel):
    """Remove targets vazios e corrige métricas inexistentes"""
    if 'targets' not in panel:
        return panel

    # Filtrar targets válidos
    valid_targets = []
    for target in panel['targets']:
        expr = target.get('expr', '').strip()

        # Pular targets vazios
        if not expr:
            continue

        # Verificar se usa métricas disponíveis
        uses_available_metric = False
        for metric in AVAILABLE_METRICS:
            if metric in expr:
                uses_available_metric = True
                break

        if uses_available_metric:
            valid_targets.append(target)

    # Se não há targets válidos mas o painel precisa de pelo menos um,
    # adicionar um comentário explicativo
    if not valid_targets and panel.get('type') in ['timeseries', 'stat', 'gauge']:
        panel['description'] = (panel.get('description', '') +
                               '\n\n⚠️ NOTA: Este painel não tem métricas disponíveis no momento. '
                               'As métricas necessárias ainda não foram implementadas.')

    panel['targets'] = valid_targets
    return panel

def process_panels(panels):
    """Processar todos os painéis recursivamente"""
    fixed_panels = []
    for panel in panels:
        # Painéis do tipo row podem conter sub-painéis
        if panel.get('type') == 'row' and 'panels' in panel:
            panel['panels'] = process_panels(panel['panels'])
        else:
            panel = fix_panel_targets(panel)

        fixed_panels.append(panel)

    return fixed_panels

def fix_dashboard(dashboard_path):
    """Corrigir o dashboard"""
    print(f"Lendo dashboard: {dashboard_path}")
    with open(dashboard_path, 'r', encoding='utf-8') as f:
        dashboard = json.load(f)

    print(f"Dashboard: {dashboard.get('title', 'Unknown')}")
    print(f"Painéis antes: {len(dashboard.get('panels', []))}")

    # Processar painéis
    if 'panels' in dashboard:
        dashboard['panels'] = process_panels(dashboard['panels'])

    print(f"Painéis depois: {len(dashboard.get('panels', []))}")

    # Salvar dashboard corrigido
    output_path = dashboard_path.replace('.json', '-fixed.json')
    print(f"Salvando dashboard corrigido: {output_path}")
    with open(output_path, 'w', encoding='utf-8') as f:
        json.dump(dashboard, f, indent=2, ensure_ascii=False)

    print("✅ Dashboard corrigido com sucesso!")
    return output_path

if __name__ == '__main__':
    dashboard_file = '/home/mateus/log_capturer_go/provisioning/dashboards/log-capturer-go-complete.json'
    fix_dashboard(dashboard_file)
