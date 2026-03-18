package registry

import (
	"fmt"
	"sync"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
)

// Manager 负责维护插件注册中心。
type Manager struct {
	mu                 sync.RWMutex
	algorithms         map[string]algorithm.Plugin
	algorithmFactories map[string]func() algorithm.Plugin
	policies           map[string]policy.Plugin
	policyFactories    map[string]func() policy.Plugin
	objectives         map[string]objective.Plugin
	objectiveFactories map[string]func() objective.Plugin
}

var defaultManager = NewManager()

// NewManager 创建新的注册中心实例。
func NewManager() *Manager {
	return &Manager{
		algorithms:         make(map[string]algorithm.Plugin),
		algorithmFactories: make(map[string]func() algorithm.Plugin),
		policies:           make(map[string]policy.Plugin),
		policyFactories:    make(map[string]func() policy.Plugin),
		objectives:         make(map[string]objective.Plugin),
		objectiveFactories: make(map[string]func() objective.Plugin),
	}
}

// Default 返回全局默认注册中心。
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
	if _, exists := m.algorithmFactories[name]; exists {
		return fmt.Errorf("algorithm=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.algorithms[name] = p
	return nil
}

func (m *Manager) registerAlgorithmFactory(name string, ctor func() algorithm.Plugin) error {
	if name == "" {
		return fmt.Errorf("algorithm factory name is empty: %w", lberrors.ErrPluginMisconfigured)
	}
	if ctor == nil {
		return fmt.Errorf("algorithm factory=%s is nil: %w", name, lberrors.ErrPluginMisconfigured)
	}
	if _, exists := m.algorithms[name]; exists {
		return fmt.Errorf("algorithm=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	if _, exists := m.algorithmFactories[name]; exists {
		return fmt.Errorf("algorithm=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.algorithmFactories[name] = ctor
	return nil
}

func (m *Manager) registerPolicy(p policy.Plugin) error {
	name := p.Name()
	if name == "" {
		return fmt.Errorf("policy plugin name is empty: %w", lberrors.ErrPluginMisconfigured)
	}
	if _, exists := m.policies[name]; exists {
		return fmt.Errorf("policy=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	if _, exists := m.policyFactories[name]; exists {
		return fmt.Errorf("policy=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.policies[name] = p
	return nil
}

func (m *Manager) registerPolicyFactory(name string, ctor func() policy.Plugin) error {
	if name == "" {
		return fmt.Errorf("policy factory name is empty: %w", lberrors.ErrPluginMisconfigured)
	}
	if ctor == nil {
		return fmt.Errorf("policy factory=%s is nil: %w", name, lberrors.ErrPluginMisconfigured)
	}
	if _, exists := m.policies[name]; exists {
		return fmt.Errorf("policy=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	if _, exists := m.policyFactories[name]; exists {
		return fmt.Errorf("policy=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.policyFactories[name] = ctor
	return nil
}

func (m *Manager) registerObjective(p objective.Plugin) error {
	name := p.Name()
	if name == "" {
		return fmt.Errorf("objective plugin name is empty: %w", lberrors.ErrPluginMisconfigured)
	}
	if _, exists := m.objectives[name]; exists {
		return fmt.Errorf("objective=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	if _, exists := m.objectiveFactories[name]; exists {
		return fmt.Errorf("objective=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.objectives[name] = p
	return nil
}

func (m *Manager) registerObjectiveFactory(name string, ctor func() objective.Plugin) error {
	if name == "" {
		return fmt.Errorf("objective factory name is empty: %w", lberrors.ErrPluginMisconfigured)
	}
	if ctor == nil {
		return fmt.Errorf("objective factory=%s is nil: %w", name, lberrors.ErrPluginMisconfigured)
	}
	if _, exists := m.objectives[name]; exists {
		return fmt.Errorf("objective=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	if _, exists := m.objectiveFactories[name]; exists {
		return fmt.Errorf("objective=%s: %w", name, lberrors.ErrDuplicatePlugin)
	}
	m.objectiveFactories[name] = ctor
	return nil
}

// RegisterAlgorithm 注册算法插件原型。
func (m *Manager) RegisterAlgorithm(p algorithm.Plugin) error {
	if p == nil {
		return fmt.Errorf("algorithm plugin is nil: %w", lberrors.ErrPluginMisconfigured)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerAlgorithm(p)
}

// RegisterAlgorithmFactory 注册算法插件工厂。
func (m *Manager) RegisterAlgorithmFactory(name string, ctor func() algorithm.Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerAlgorithmFactory(name, ctor)
}

// RegisterPolicy 注册策略插件原型。
func (m *Manager) RegisterPolicy(p policy.Plugin) error {
	if p == nil {
		return fmt.Errorf("policy plugin is nil: %w", lberrors.ErrPluginMisconfigured)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerPolicy(p)
}

// RegisterPolicyFactory 注册策略插件工厂。
func (m *Manager) RegisterPolicyFactory(name string, ctor func() policy.Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerPolicyFactory(name, ctor)
}

// RegisterObjective 注册目标函数插件原型。
func (m *Manager) RegisterObjective(p objective.Plugin) error {
	if p == nil {
		return fmt.Errorf("objective plugin is nil: %w", lberrors.ErrPluginMisconfigured)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerObjective(p)
}

// RegisterObjectiveFactory 注册目标函数插件工厂。
func (m *Manager) RegisterObjectiveFactory(name string, ctor func() objective.Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerObjectiveFactory(name, ctor)
}

// MustRegisterAlgorithm 注册算法插件，失败时 panic。
func (m *Manager) MustRegisterAlgorithm(p algorithm.Plugin) {
	if err := m.RegisterAlgorithm(p); err != nil {
		panic(err)
	}
}

// MustRegisterAlgorithmFactory 注册算法插件工厂，失败时 panic。
func (m *Manager) MustRegisterAlgorithmFactory(name string, ctor func() algorithm.Plugin) {
	if err := m.RegisterAlgorithmFactory(name, ctor); err != nil {
		panic(err)
	}
}

// MustRegisterPolicy 注册策略插件，失败时 panic。
func (m *Manager) MustRegisterPolicy(p policy.Plugin) {
	if err := m.RegisterPolicy(p); err != nil {
		panic(err)
	}
}

// MustRegisterPolicyFactory 注册策略插件工厂，失败时 panic。
func (m *Manager) MustRegisterPolicyFactory(name string, ctor func() policy.Plugin) {
	if err := m.RegisterPolicyFactory(name, ctor); err != nil {
		panic(err)
	}
}

// MustRegisterObjective 注册目标函数插件，失败时 panic。
func (m *Manager) MustRegisterObjective(p objective.Plugin) {
	if err := m.RegisterObjective(p); err != nil {
		panic(err)
	}
}

// MustRegisterObjectiveFactory 注册目标函数插件工厂，失败时 panic。
func (m *Manager) MustRegisterObjectiveFactory(name string, ctor func() objective.Plugin) {
	if err := m.RegisterObjectiveFactory(name, ctor); err != nil {
		panic(err)
	}
}

// GetAlgorithm 获取算法插件原型。
func (m *Manager) GetAlgorithm(name string) (algorithm.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.algorithms[name]
	return p, ok
}

// GetAlgorithmFactory 获取算法插件工厂。
func (m *Manager) GetAlgorithmFactory(name string) (func() algorithm.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ctor, ok := m.algorithmFactories[name]
	return ctor, ok
}

// GetPolicy 获取策略插件原型。
func (m *Manager) GetPolicy(name string) (policy.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[name]
	return p, ok
}

// GetPolicyFactory 获取策略插件工厂。
func (m *Manager) GetPolicyFactory(name string) (func() policy.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ctor, ok := m.policyFactories[name]
	return ctor, ok
}

// GetObjective 获取目标函数插件原型。
func (m *Manager) GetObjective(name string) (objective.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.objectives[name]
	return p, ok
}

// GetObjectiveFactory 获取目标函数插件工厂。
func (m *Manager) GetObjectiveFactory(name string) (func() objective.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ctor, ok := m.objectiveFactories[name]
	return ctor, ok
}

// HasAlgorithm 判断算法插件名是否已注册（原型或工厂）。
func (m *Manager) HasAlgorithm(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.algorithms[name]; ok {
		return true
	}
	_, ok := m.algorithmFactories[name]
	return ok
}

// HasPolicy 判断策略插件名是否已注册（原型或工厂）。
func (m *Manager) HasPolicy(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.policies[name]; ok {
		return true
	}
	_, ok := m.policyFactories[name]
	return ok
}

// HasObjective 判断目标函数插件名是否已注册（原型或工厂）。
func (m *Manager) HasObjective(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.objectives[name]; ok {
		return true
	}
	_, ok := m.objectiveFactories[name]
	return ok
}

// RegisterAlgorithm 注册算法插件到默认注册中心。
func RegisterAlgorithm(p algorithm.Plugin) error {
	return defaultManager.RegisterAlgorithm(p)
}

// RegisterAlgorithmFactory 注册算法插件工厂到默认注册中心。
func RegisterAlgorithmFactory(name string, ctor func() algorithm.Plugin) error {
	return defaultManager.RegisterAlgorithmFactory(name, ctor)
}

// RegisterPolicy 注册策略插件到默认注册中心。
func RegisterPolicy(p policy.Plugin) error {
	return defaultManager.RegisterPolicy(p)
}

// RegisterPolicyFactory 注册策略插件工厂到默认注册中心。
func RegisterPolicyFactory(name string, ctor func() policy.Plugin) error {
	return defaultManager.RegisterPolicyFactory(name, ctor)
}

// RegisterObjective 注册目标函数插件到默认注册中心。
func RegisterObjective(p objective.Plugin) error {
	return defaultManager.RegisterObjective(p)
}

// RegisterObjectiveFactory 注册目标函数插件工厂到默认注册中心。
func RegisterObjectiveFactory(name string, ctor func() objective.Plugin) error {
	return defaultManager.RegisterObjectiveFactory(name, ctor)
}

// MustRegisterAlgorithm 注册算法插件到默认注册中心，失败时 panic。
func MustRegisterAlgorithm(p algorithm.Plugin) {
	defaultManager.MustRegisterAlgorithm(p)
}

// MustRegisterAlgorithmFactory 注册算法插件工厂到默认注册中心，失败时 panic。
func MustRegisterAlgorithmFactory(name string, ctor func() algorithm.Plugin) {
	defaultManager.MustRegisterAlgorithmFactory(name, ctor)
}

// MustRegisterPolicy 注册策略插件到默认注册中心，失败时 panic。
func MustRegisterPolicy(p policy.Plugin) {
	defaultManager.MustRegisterPolicy(p)
}

// MustRegisterPolicyFactory 注册策略插件工厂到默认注册中心，失败时 panic。
func MustRegisterPolicyFactory(name string, ctor func() policy.Plugin) {
	defaultManager.MustRegisterPolicyFactory(name, ctor)
}

// MustRegisterObjective 注册目标函数插件到默认注册中心，失败时 panic。
func MustRegisterObjective(p objective.Plugin) {
	defaultManager.MustRegisterObjective(p)
}

// MustRegisterObjectiveFactory 注册目标函数插件工厂到默认注册中心，失败时 panic。
func MustRegisterObjectiveFactory(name string, ctor func() objective.Plugin) {
	defaultManager.MustRegisterObjectiveFactory(name, ctor)
}

// GetAlgorithm 获取默认注册中心中的算法插件原型。
func GetAlgorithm(name string) (algorithm.Plugin, bool) {
	return defaultManager.GetAlgorithm(name)
}

// GetAlgorithmFactory 获取默认注册中心中的算法插件工厂。
func GetAlgorithmFactory(name string) (func() algorithm.Plugin, bool) {
	return defaultManager.GetAlgorithmFactory(name)
}

// GetPolicy 获取默认注册中心中的策略插件原型。
func GetPolicy(name string) (policy.Plugin, bool) {
	return defaultManager.GetPolicy(name)
}

// GetPolicyFactory 获取默认注册中心中的策略插件工厂。
func GetPolicyFactory(name string) (func() policy.Plugin, bool) {
	return defaultManager.GetPolicyFactory(name)
}

// GetObjective 获取默认注册中心中的目标函数插件原型。
func GetObjective(name string) (objective.Plugin, bool) {
	return defaultManager.GetObjective(name)
}

// GetObjectiveFactory 获取默认注册中心中的目标函数插件工厂。
func GetObjectiveFactory(name string) (func() objective.Plugin, bool) {
	return defaultManager.GetObjectiveFactory(name)
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
