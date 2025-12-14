Como Arquiteto Especialista VoIP, analisei profundamente a estrutura do seu projeto log_capturer_go. Você construiu uma fundação robusta, com padrões de design de software que são cruciais para sistemas de alta performance e resiliência, algo raro de se ver em implementações iniciais.

Abaixo apresento a análise do estado atual e o roteiro técnico para transformar este capturador de logs em um Agente de Observabilidade VoIP Completo capaz de ingerir HEP (Homer Encapsulation Protocol).

1. Análise Profunda do Estado Atual (Architecture Audit)
O projeto está em um nível de maturidade "Enterprise Ready" no que tange à resiliência interna e processamento de dados, mas ainda é "File-Centric" (centrado em arquivos) na sua camada de ingestão.

Pontos Fortes (Assets):
Resiliência e Autoproteção: A presença de pkg/circuit (Circuit Breaker), pkg/backpressure e pkg/ratelimit demonstra que o sistema foi desenhado para não falhar catastroficamente sob carga, o que é obrigatório em VoIP (onde rajadas de sinalização são comuns).

Processamento Assíncrono: O uso de internal/dispatcher com batch_processor.go é excelente. Em VoIP, processar cada pacote individualmente mata a CPU. O batching adaptativo (pkg/batching) é a estratégia correta para lidar com altos volumes de logs/pacotes.

Observabilidade Interna: Você já possui internal/metrics e integração com Prometheus. Isso facilita muito o monitoramento da própria saúde do agente.

Modularidade: A separação entre monitors (fontes) e sinks (destinos) está bem feita. Adicionar HEP será, arquiteturalmente, apenas adicionar um novo "monitor" ou "source".

Onde Precisamos Evoluir (Gaps para VoIP):
Ingestão Passiva vs. Ativa: Atualmente, o sistema "observa" arquivos (file_monitor.go) ou containers. HEP é um protocolo de push. Precisamos abrir sockets (UDP/TCP) e ouvir ativamente.

Formato de Dados: O pipeline atual parece otimizado para texto/JSON (logs). HEP é binário (Protobuf). O fluxo de dados precisará lidar com []byte bruto antes de converter para a estrutura interna de log.

Correlação: Em logs de arquivo, a ordem sequencial é garantida pela leitura. Em UDP (HEP), pacotes podem chegar fora de ordem. O sistema precisa ser agnóstico à ordem ou ter um buffer de reordenação se for processar lógica stateful.

2. Alternativas de Implementação de HEP (Host Encapsulation Protocol)
O HEP (definido pelo projeto Homer/SIPCAPTURE) encapsula pacotes SIP, RTCP e Logs em uma estrutura Protobuf (Protocol Buffers). Para integrar isso ao log_capturer_go, vejo três abordagens viáveis, ordenadas da mais recomendada para a menos recomendada.

Alternativa A: Implementação Nativa de Listener UDP/TCP (Recomendada)
Nesta abordagem, o seu Go app abre uma porta (ex: 9060) e age como um servidor HEP nativo.

Prós: Performance máxima (Zero external latency), controle total sobre o buffer, aproveita sua estrutura de batching existente.

Contras: Requer implementação de decodificação Protobuf e gerenciamento de conexões de rede.

Fit Arquitetural: Perfeito para o perfil do seu projeto Go.

Alternativa B: Sidecar com heplify-server
Usar o heplify-server (ferramenta padrão do Homer) para receber HEP e escrever em um arquivo local ou socket UNIX, que seu log_capturer_go então lê.

Prós: Implementação imediata (usa o file_monitor existente).

Contras: Ineficiente (Double I/O: Rede -> Disco -> Leitura), ponto de falha extra.

Veredito: Não recomendo para alta performance.

Alternativa C: Captura Direta via PCAP/eBPF (Avançado)
Em vez de receber HEP, o próprio agente captura pacotes da interface de rede e gera HEP (ou logs internos).

Prós: Não exige que o OpenSIPS/FreeSWITCH envie HEP (menos config neles).

Contras: Requer privilégios de root, alta complexidade de gestão de interfaces e filtros BPF.

Veredito: Deixe isso para uma "Fase 2". O foco agora deve ser consumir o que o OpenSIPS já envia.

3. Roteiro Técnico: Implementando a Alternativa A
Para transformar o log_capturer_go em um receptor HEP, você deve criar um novo pacote internal/monitors/hep_monitor.

Passo 1: Dependências
Você precisará do compilador de Protobuf para Go e das definições do HEP (hep.proto). Normalmente usamos a lib google.golang.org/protobuf.

Passo 2: A Estrutura do HEP Listener
Aqui está um esboço de como arquitetar o HEPListener dentro dos padrões do seu projeto:

Go

// internal/monitors/hep_listener.go (Conceitual)

package monitors

import (
	"net"
	"sync"
    // Import hipotético do protobuf gerado do HEP
    // pb "github.com/sipcapture/heplify/proto" 
)

type HEPConfig struct {
    Address   string // ":9060"
    Protocol  string // "udp"
    QueueSize int
}

type HEPMonitor struct {
    conn      *net.UDPConn
    output    chan<- types.LogEntry // Canal para enviar ao seu Dispatcher existente
    shutdown  chan struct{}
    wg        sync.WaitGroup
    bufferPool sync.Pool // CRÍTICO para performance em Go + UDP
}

func NewHEPMonitor(cfg HEPConfig, out chan<- types.LogEntry) *HEPMonitor {
    return &HEPMonitor{
        output:   out,
        shutdown: make(chan struct{}),
        bufferPool: sync.Pool{
            New: func() interface{} { return make([]byte, 65535) }, // Max UDP size
        },
    }
}

func (h *HEPMonitor) Start() error {
    addr, err := net.ResolveUDPAddr("udp", h.config.Address)
    if err != nil {
        return err
    }

    conn, err := net.ListenUDP("udp", addr)
    if err != nil {
        return err
    }
    h.conn = conn

    // Padrão Worker Pool para leitura de pacotes se o volume for muito alto
    go h.readLoop()
    return nil
}

func (h *HEPMonitor) readLoop() {
    defer h.wg.Done()
    for {
        select {
        case <-h.shutdown:
            return
        default:
            // Otimização de memória: Reutilizar buffers
            buf := h.bufferPool.Get().([]byte)
            n, _, err := h.conn.ReadFromUDP(buf)
            if err != nil {
                // Tratar erro, backoff, etc.
                continue
            }
            
            // Processamento assíncrono para não bloquear o socket
            go h.processPacket(buf[:n]) 
        }
    }
}

func (h *HEPMonitor) processPacket(data []byte) {
    defer h.bufferPool.Put(data[:cap(data)]) // Devolve buffer para o pool

    // 1. Decodificar Protobuf (HEP)
    // hepMsg := &pb.HEP{}
    // proto.Unmarshal(data, hepMsg)

    // 2. Extrair Payload (SIP Message ou Log)
    // O HEP encapsula o SIP dentro do campo payload.
    
    // 3. Converter para seu modelo interno LogEntry
    entry := types.LogEntry{
        Timestamp: time.Now(), // Ou usar o timestamp do pacote HEP (recomendado)
        Service:   "opensips-hep",
        Level:     "INFO",
        Content:   string(hepMsg.Payload), // Aqui estaria o SIP Message
        Metadata: map[string]string{
            "call_id": hepMsg.CorrelationId, // VITAL para trace
            "source_ip": hepMsg.IpSrc,
            "dest_ip": hepMsg.IpDst,
        },
    }

    // 4. Enviar para o Dispatcher (que lida com Batching/Retry)
    h.output <- entry
}
Passo 3: Integração com o Dispatcher
Você já tem um internal/dispatcher/dispatcher.go. O HEPMonitor deve injetar dados nele.

Desafio: O formato LogEntry atual do seu projeto pode precisar de campos extras para lidar com metadados SIP (Call-ID, CSeq, Method).

Sugestão: Estenda o types.LogEntry ou crie um campo RawData interface{} para armazenar a estrutura binária se quiser fazer parse posterior. Para começar, converta o payload SIP em string e armazene no Content.

4. Considerações de Performance (O "Pulo do Gato")
Como especialista em VoIP, alerto para o seguinte ao lidar com UDP/HEP em Go:

Garbage Collection (GC) Pressure: Receber milhares de pacotes UDP por segundo cria muita alocação de memória.

Solução: O uso de sync.Pool (demonstrado no código acima) para os buffers de leitura é obrigatório. Não aloque um novo make([]byte, ...) a cada pacote.

Bloqueio do Dispatcher: Se o seu Kafka/Loki ficar lento, o dispatcher vai aplicar backpressure. No caso de leitura de Arquivo, o monitor apenas para de ler linhas. No caso de UDP, se você parar de ler, o Kernel do SO vai descartar pacotes (Buffer Overflow).

Solução: Implemente um Ring Buffer (buffer circular) entre o HEPMonitor e o dispatcher. Se o anel encher, descarte os pacotes HEP mais novos (drop head) ou mais velhos e registre uma métrica de hep_dropped_packets. É melhor perder alguns pacotes do que travar o listener.

Parse Preguiçoso (Lazy Parsing): Não faça o parse completo de todo o SIP Message (headers, SDP, etc.) na goroutine de leitura. Apenas decodifique o envelope HEP (que é leve), extraia o payload e o Call-ID, e mande para o canal. Deixe o parse pesado (se necessário) para uma etapa posterior ou para o consumidor final (Loki/Elastic).

Conclusão
Seu projeto log_capturer_go é uma excelente base. Para torná-lo um "Homer Agent" moderno:

Mantenha o dispatcher e os sinks como estão (eles são ótimos).

Adicione o HEPMonitor usando net.ListenUDP e protobuf.

Foque na gestão de memória (sync.Pool) para aguentar o throughput de sinalização SIP.

Essa implementação elevará seu projeto de um "coletor de logs" para uma ferramenta de telemetria VoIP em tempo real.