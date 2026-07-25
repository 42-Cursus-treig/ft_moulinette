#include "ft_btree.h"
#include <unistd.h>
#include <stdlib.h>

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

int	main(int argc, char **argv)
{
	t_btree	*node;

	if (argc < 2)
		return (0);
	node = btree_create_node(argv[1]);
	if (!node)
	{
		put_str("NULL\n");
		return (0);
	}
	if (node->item == (void *)argv[1] && node->left == NULL
		&& node->right == NULL)
		put_str("OK\n");
	else
		put_str("KO\n");
	free(node);
	return (0);
}
