kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: rotor
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: rotor
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: rotor
subjects:
- kind: ServiceAccount
  name: rotor
  namespace: default # Change this if you are working in a different namespace.
roleRef:
  kind: ClusterRole
  name: rotor
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: rotor
spec:
  template:
    metadata:
      labels:
        run: rotor
    spec:
      serviceAccountName: rotor
      containers:
      - image: turbinelabs/rotor:0.16.0-rc1
        imagePullPolicy: Always
        name: rotor
        ports:
          - containerPort: 50000 # xDS server is exposed here
        env:
        - name: TBNCOLLECT_CMD
          value: kubernetes
---
apiVersion: v1
kind: Service
metadata:
  labels:
    run: rotor
  name: rotor
spec:
  ports:
  - port: 50000
    protocol: TCP
    targetPort: 50000
  selector:
    run: rotor
  type: ClusterIP
