package monitor

const InstructionFilePath = "/tmp/mico_aivs_lab/instruction.log"

type InstructionMonitor struct {
	fileMonitor *FileMonitor
}

func NewInstructionMonitor() *InstructionMonitor {
	return &InstructionMonitor{
		fileMonitor: NewFileMonitor(),
	}
}

func (im *InstructionMonitor) Start(onUpdate func(FileMonitorEvent)) {
	im.fileMonitor.Start(InstructionFilePath, onUpdate)
}

func (im *InstructionMonitor) Stop() {
	im.fileMonitor.Stop()
}
