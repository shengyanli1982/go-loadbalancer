package registry

import (
	"fmt"
	"sync"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
)

// Manager 负责维护插件注册表。
type Manager struct {
	mu         sync.RWMutex
	algorithms map[string]algorithm.Plugin
	policies   map[string]policy.Plugin
	objectives map[string]objective.Plugin
}

var defaultManager = NewManager()

// NewManager 创建新的插件注册管理器。
func NewManager() *Manager {
	return &Manager{
		algorithms: make(map[string]algorithm.Plugin),
		policies:   make(map[string]policy.Plugin),
		objectives: make(map[string]objective.Plugin),
	}
}

// Default 返回全局默认注册管理器。
func Default() *Manager {
	return defaultManager
}

func (m *Manager) registerAlgorithm(p algorithm.Plugin) error {
	name := p.Name()
	if name == "" {
		return fmt.Errorf("algorithm plugin name is empty: %w", lberrors.ErrPluginMisconfigured)
	}
	if _, exists := m.algorithms[name]; exists {
		return fmt.Errorf("algorithm=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.algorithms[name] = p
	return nil
}

// RegisterAlgorithm 注册算法插件。
func (m *Manager) RegisterAlgorithm(p algorithm.Plugin) error {
	if p == nil {
		return fmt.Errorf("algorithm plugin is nil: %w", lberrors.ErrPluginMisconfigured)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerAlgorithm(p)
}

// RegisterPolicy 注册策略插件。
func (m *Manager) RegisterPolicy(p policy.Plugin) error {
	if p == nil {
		return fmt.Errorf("policy plugin is nil: %w", lberrors.ErrPluginMisconfigured)
	}
	name := p.Name()
	if name == "" {
		return fmt.Errorf("policy plugin name is empty: %w", lberrors.ErrPluginMisconfigured)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.policies[name]; exists {
		return fmt.Errorf("policy=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.policies[name] = p
	return nil
}

// RegisterObjective 注册目标函数插件。
func (m *Manager) RegisterObjective(p objective.Plugin) error {
	if p == nil {
		return fmt.Errorf("objective plugin is nil: %w", lberrors.ErrPluginMisconfigured)
	}
	name := p.Name()
	if name == "" {
		return fmt.Errorf("objective plugin name is empty: %w", lberrors.ErrPluginMisconfigured)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.objectives[name]; exists {
		return fmt.Errorf("objective=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.objectives[name] = p
	return nil
}

// MustRegisterAlgorithm 注册算法插件，失败时 panic。
func (m *Manager) MustRegisterAlgorithm(p algorithm.Plugin) {
	if err := m.RegisterAlgorithm(p); err != nil {
		panic(err)
	}
}

// MustRegisterPolicy 注册策略插件，失败时 panic。
func (m *Manager) MustRegisterPolicy(p policy.Plugin) {
	if err := m.RegisterPolicy(p); err != nil {
		panic(err)
	}
}

// MustRegisterObjective 注册目标函数插件，失败时 panic。
func (m *Manager) MustRegisterObjective(p objective.Plugin) {
	if err := m.RegisterObjective(p); err != nil {
		panic(err)
	}
}

// GetAlgorithm 获取算法插件。
func (m *Manager) GetAlgorithm(name string) (algorithm.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.algorithms[name]
	return p, ok
}

// GetPolicy 获取策略插件。
func (m *Manager) GetPolicy(name string) (policy.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[name]
	return p, ok
}

// GetObjective 获取目标函数插件。
func (m *Manager) GetObjective(name string) (objective.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.objectives[name]
	return p, ok
}

// HasAlgorithm 判断算法插件是否存在。
func (m *Manager) HasAlgorithm(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.algorithms[name]
	return ok
}

// HasPolicy 判断策略插件是否存在。
func (m *Manager) HasPolicy(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.policies[name]
	return ok
}

// HasObjective 判断目标函数插件是否存在。
func (m *Manager) HasObjective(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.objectives[name]
	return ok
}

// RegisterAlgorithm 注册算法插件到默认注册中心。
func RegisterAlgorithm(p algorithm.Plugin) error {
	return defaultManager.RegisterAlgorithm(p)
}

// RegisterPolicy 注册策略插件到默认注册中心。
func RegisterPolicy(p policy.Plugin) error {
	return defaultManager.RegisterPolicy(p)
}

// RegisterObjective 注册目标函数插件到默认注册中心。
func RegisterObjective(p objective.Plugin) error {
	return defaultManager.RegisterObjective(p)
}

// MustRegisterAlgorithm 注册算法插件到默认注册中心。
func MustRegisterAlgorithm(p algorithm.Plugin) {
	defaultManager.MustRegisterAlgorithm(p)
}

// MustRegisterPolicy 注册策略插件到默认注册中心。
func MustRegisterPolicy(p policy.Plugin) {
	defaultManager.MustRegisterPolicy(p)
}

// MustRegisterObjective 注册目标函数插件到默认注册中心。
func MustRegisterObjective(p objective.Plugin) {
	defaultManager.MustRegisterObjective(p)
}

// GetAlgorithm 获取默认注册中心中的算法插件。
func GetAlgorithm(name string) (algorithm.Plugin, bool) {
	return defaultManager.GetAlgorithm(name)
}

// GetPolicy 获取默认注册中心中的策略插件。
func GetPolicy(name string) (policy.Plugin, bool) {
	return defaultManager.GetPolicy(name)
}

// GetObjective 获取默认注册中心中的目标函数插件。
func GetObjective(name string) (objective.Plugin, bool) {
	return defaultManager.GetObjective(name)
}

// HasAlgorithm 判断默认注册中心中是否存在算法插件。
func HasAlgorithm(name string) bool {
	return defaultManager.HasAlgorithm(name)
}

// HasPolicy 判断默认注册中心中是否存在策略插件。
func HasPolicy(name string) bool {
	return defaultManager.HasPolicy(name)
}

// HasObjective 判断默认注册中心中是否存在目标函数插件。
func HasObjective(name string) bool {
	return defaultManager.HasObjective(name)
}
